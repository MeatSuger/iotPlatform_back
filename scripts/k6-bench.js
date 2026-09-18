// ============================================================
// IoT Platform 综合压测（唯一 k6 脚本）
//
// 按真实流量模型设计 4 类场景：
//   1. devices   —— 设备侧：数据上报 + 心跳 + 偶尔拉取下行命令（占大头）
//   2. app_users —— App 用户：登录态检查 → 设备列表 → 设备详情 → 历史曲线 → 个人资料 + 偶尔更新设备
//   3. ws_users  —— App 保持的 WebSocket 长连接（/api/ws/user）
//   4. auth_flow —— 低频 登录/登出 配对（默认 test/123456，可用 USER_ACCOUNT/USER_PASSWD 覆盖）
//
// 关键约定（与项目代码核对过）：
//   - 业务成功码是响应体 code===200（pkg/common/response.go CodeSuccess=200），不是 0
//   - 服务端业务出错也返回 HTTP 200（code:400/401/...），http_req_failed 抓不到，
//     因此用自定义指标 biz_req_failed 作为业务失败率兜底阈值
//   - 上报 DTO 中 sensors[].type 为必填（internal/model/sensor_data.go binding:"required"）
//   - WS 握手不计入 http_req_* 指标（k6/ws 独立产出 ws_* 指标），勿对其设 http 阈值
//   - 登录响应字段是 data.tokenValue（Sa-Token 风格），不是 data.token
//
// ============================================================
// 【2026-09 修复记录】为什么之前一跑就是 biz_req_failed 99.7%
// ============================================================
// 症状：http_req_failed=0%（HTTP 全 200），但 biz_req_failed=99.7%，
//       只有 login ok / logout ok 通过，其余校验全 0%。
//
// 根因（两个叠加）：
//   1) 旧脚本硬编码 USER_TOKEN / DEVICE_TOKEN。用户 Token 有效期只有 3 天，
//      设备 Token 在设备 30 天未上线后会被定时任务清理 —— 两个都失效了。
//      于是所有带 token 的请求都返回 HTTP 200 + code:401。
//      login/logout 之所以通过，是因为它们用「账号密码」，不依赖 token。
//   2) 服务端 MaxLoginCount=5 且 Sa-Token 同端互斥：不带 device 字段登录时，
//      服务端回退用 User-Agent 生成设备端标识，k6 的 UA 全都一样 ⇒
//      auth_flow 反复登录会把主 Token 一起顶掉。
//   3) 演示账号 test/123456 名下**0 台设备**。设备侧走 DeviceIDAuth 不受影响，
//      但 userTraffic 的设备详情会因归属校验返回 HTTP 200 + code:403，
//      报告照样显示"业务失败"。必须改用**拥有设备的真实账号**。
//
// 修复：
//   - 新增 setup()，**动态获取** User Token（账号密码登录）与 Device Token
//     （GET /devices/{id}/token 只需 6 位 hex 设备 ID，无需其他凭证）。
//   - setup() 里做**三重预校验**：(a) 账号名下必须有设备；(b) 用户 Token 的
//     isLogin 必须为 true；(c) 设备 Token 能通过 /commands 鉴权。
//     任一不满足立即 fail() 中止，杜绝"跑满 2 分钟才发现全是假失败"。
//   - auth_flow 使用**独立的 device 标识**，与主会话分属不同设备端，互不顶号。
//   - 仍支持 USER_TOKEN / DEVICE_TOKEN 环境变量覆盖（调试用）。
//
// 附带修复：endpoint:update 之前 0 样本，是因为 list 校验失败后 did 取不到值、
//           update 分支根本没进去；Token 恢复后该分支会正常执行。
//
// 用法:
//   k6 run scripts/k6-bench.js                       # 默认 100 VU 打生产（远端自动放宽延迟阈值）
//   VUS=300 k6 run scripts/k6-bench.js               # 加压
//   BASE_URL=http://127.0.0.1:9091/api k6 run scripts/k6-bench.js   # 直连后端（严格阈值）
//   USER_ACCOUNT=xxx USER_PASSWD=yyy k6 run scripts/k6-bench.js     # 指定登录账号
//   SKIP_SETUP_CHECK=1 k6 run scripts/k6-bench.js    # 跳过 setup 预校验（不推荐）
// ============================================================
import http from 'k6/http';
import { check, sleep, fail } from 'k6';
import { Rate } from 'k6/metrics';
import ws from 'k6/ws';

// ---------- 环境配置 ----------
const BASE = __ENV.BASE_URL || 'https://api.meatsuger.top/api';
const WS_BASE = BASE.replace(/^http/, 'ws');
const DEVICE_ID = __ENV.DEVICE_ID || '369c04';
const USER_ACCOUNT = __ENV.USER_ACCOUNT || 'test';
const USER_PASSWD = __ENV.USER_PASSWD || '123456';
const VUS = parseInt(__ENV.VUS || '100', 10);
const SKIP_SETUP_CHECK = __ENV.SKIP_SETUP_CHECK === '1';

// 可选覆盖：仅在调试或账号不可用时使用。
// 正常情况留空 —— setup() 会自动获取，不会再出现「token 过期」导致的满盘皆输。
const ENV_USER_TOKEN = __ENV.USER_TOKEN || '';
const ENV_DEVICE_TOKEN = __ENV.DEVICE_TOKEN || '';

// 远端目标要叠加公网 RTT（实测中位 ~86ms），本地直连才用严格 SLO
const IS_LOCAL = /127\.0\.0\.1|localhost/.test(BASE);
const NET = IS_LOCAL ? 0 : 250;

// ---------- 后端处理时间预算（不含网络 RTT，单位 ms） ----------
const SLO_CACHE = 150; // 纯缓存/内存/轻量操作
const SLO_READ = 200; // 简单读 / 缓存命中
const SLO_WRITE = 300; // 写库 + 缓存一致性（Write-Through）
const SLO_QUERY = 500; // 复杂查询 / 外部存储（InfluxDB）
const SLO_AUTH = 800; // 认证：密码哈希 + Token 签发

// 业务级失败率：HTTP 200 但响应体 code !== 200
const bizFail = new Rate('biz_req_failed');

// ---------- VU 分配（按真实流量比例） ----------
const nDevices = Math.max(1, Math.round(VUS * 0.6)); // 设备流量占大头
const nUsers = Math.max(1, Math.round(VUS * 0.3)); // App 用户
const nWs = VUS >= 10 ? 2 : 1; // WS 长连接（少量即可，模拟常驻连接）

// 爬升→稳定→回落
function ramp(n) {
  return [
    { duration: '30s', target: n },
    { duration: '1m', target: n },
    { duration: '30s', target: 0 },
  ];
}

export const options = {
  scenarios: {
    devices: {
      executor: 'ramping-vus',
      exec: 'deviceTraffic',
      stages: ramp(nDevices),
    },
    app_users: {
      executor: 'ramping-vus',
      exec: 'userTraffic',
      stages: ramp(nUsers),
      startTime: '5s',
    },
    ws_users: {
      executor: 'constant-vus',
      exec: 'wsTraffic',
      vus: nWs,
      duration: '1m30s',
      startTime: '10s',
      gracefulStop: '10s',
    },
    auth_flow: {
      executor: 'constant-vus',
      exec: 'authFlow',
      vus: 1,
      duration: '1m30s',
      startTime: '15s',
    },
  },
  thresholds: {
    // 设备侧（核心热路径）
    'http_req_duration{endpoint:report}': [`p(95)<${SLO_WRITE + NET}`], // 上报：校验 + 写入 + 状态
    'http_req_duration{endpoint:ping}': [`p(95)<${SLO_CACHE + NET}`], // 心跳：轻量 DB 更新
    'http_req_duration{endpoint:cmd}': [`p(95)<${SLO_CACHE + NET}`], // 命令拉取：Redis
    // App 用户侧
    'http_req_duration{endpoint:islogin}': [`p(95)<${SLO_CACHE + NET}`], // 登录态：Redis
    'http_req_duration{endpoint:list}': [`p(95)<${SLO_READ + NET}`], // 设备列表：L1/L2/DB
    'http_req_duration{endpoint:detail}': [`p(95)<${SLO_READ + NET}`], // 设备详情：缓存 + 状态 Hash
    'http_req_duration{endpoint:update}': [`p(95)<${SLO_WRITE + NET}`], // 更新：写库 + 缓存一致性
    'http_req_duration{endpoint:profile}': [`p(95)<${SLO_READ + NET}`], // 用户资料：缓存
    // 历史曲线：缓存未命中时走 InfluxDB（复杂查询）
    'http_req_duration{endpoint:history}': [`p(95)<${SLO_QUERY + NET}`],
    // 认证流程（密码哈希 + Token 签发）
    'http_req_duration{endpoint:login}': [`p(95)<${SLO_AUTH + NET}`],
    // 注意：不给 {endpoint:ws} 设 http 阈值 —— ws 握手不计入 http_req_*，设了也是空指标
    http_req_failed: ['rate<0.05'],
    // 业务失败率兜底：服务端出错也返回 HTTP 200（code:400/401），http_req_failed 看不出来
    biz_req_failed: ['rate<0.05'],
    // RPS sanity check：随 VUS 缩放（原固定 >50 在 VUS<35 时必误报）
    http_reqs: [`rate>${Math.max(1, Math.floor(VUS * 0.3))}`],
  },
  summaryTrendStats: ['avg', 'min', 'med', 'max', 'p(90)', 'p(95)', 'p(99)'],
};

// ---------- 工具 ----------

// 业务成功：HTTP 200 且响应体 code===200（pkg/common/response.go CodeSuccess=200；
// 服务端业务错误也是 HTTP 200 + code:400/401/...，不能只看状态码）
function bizOk(r) {
  if (r.status !== 200) return false;
  try {
    return r.json('code') === 200;
  } catch (e) {
    return false;
  }
}

// 安全取响应体 code（拿不到就返回 undefined）
function bodyCode(r) {
  try {
    return r.json('code');
  } catch (e) {
    return undefined;
  }
}

// 带业务失败率埋点的 check（喂给 biz_req_failed 阈值）
function checkBiz(r, label) {
  const ok = bizOk(r);
  bizFail.add(!ok);
  check(r, { [label]: () => ok });
  return ok;
}

const mask = (t) => (t ? `${String(t).slice(0, 8)}…` : '(empty)');

// ============================================================
// setup：动态获取并预校验 Token（本次修复的核心）
// ============================================================
// k6 会把 setup() 的返回值作为第一个参数传给每个 exec 函数。
//
// 为什么要预校验：旧脚本的问题是「跑满 2 分钟才发现所有 token 都是坏的」。
// 现在只要任一 Token 不可用就 fail()，几秒内中止并打印明确原因。
export function setup() {
  console.log(`[setup] target = ${BASE}${IS_LOCAL ? '  (本地直连，严格阈值)' : '  (远端，阈值 +' + NET + 'ms RTT)'}`);

  // ---------- 1) 用户 Token ----------
  let userToken = ENV_USER_TOKEN;
  if (userToken) {
    console.log('[setup] 使用环境变量 USER_TOKEN 覆盖');
  } else {
    const login = http.post(
      `${BASE}/users/login`,
      JSON.stringify({
        account: USER_ACCOUNT,
        passwd: USER_PASSWD,
        // ⚠️ 关键：显式传 device 标识。
        //    服务端 MaxLoginCount=5 且同端互斥；不传 device 时回退用 User-Agent，
        //    而 k6 的 UA 完全相同 ⇒ auth_flow 的登录会把主会话顶掉。
        device: 'k6-bench-main',
      }),
      { headers: { 'Content-Type': 'application/json' }, tags: { endpoint: 'setup' } }
    );

    if (!bizOk(login)) {
      fail(
        `[setup] 用户登录失败，无法继续压测。\n` +
          `  HTTP ${login.status}  code=${bodyCode(login)}\n` +
          `  body=${String(login.body).slice(0, 300)}\n` +
          `  排查：账号 ${USER_ACCOUNT} 是否存在/密码是否正确（可用 USER_ACCOUNT / USER_PASSWD 覆盖）；\n` +
          `        若账号被锁或服务未启动，请先修好再压测。`
      );
    }
    try {
      userToken = login.json('data').tokenValue;
    } catch (e) {
      userToken = '';
    }
    if (!userToken) {
      fail(`[setup] 登录成功但响应缺少 data.tokenValue，body=${String(login.body).slice(0, 300)}`);
    }
    console.log(`[setup] 用户登录成功 userToken=${mask(userToken)}`);
  }

  // ---------- 2) 设备 ID（从用户设备列表取真实归属设备） ----------
  // ⚠️ 必须用**拥有设备的真实账号**。演示账号 test/123456 名下 0 台设备，
  //    此时 userTraffic 的设备详情/更新会因归属校验返回 403，报告会再次出现假失败。
  let deviceId = DEVICE_ID;
  let listEmpty = false;
  const list = http.get(`${BASE}/devices`, {
    headers: { Authorization: userToken },
    tags: { endpoint: 'setup' },
  });
  if (bizOk(list)) {
    try {
      const arr = list.json('data');
      if (Array.isArray(arr) && arr.length > 0 && arr[0].deviceId) {
        deviceId = arr[0].deviceId;
        console.log(`[setup] 设备列表 ${arr.length} 台，选用 deviceId=${deviceId}`);
      } else {
        listEmpty = true;
        console.warn(`[setup] ⚠️ 账号 ${USER_ACCOUNT} 名下没有任何设备，回退 DEVICE_ID=${deviceId}`);
      }
    } catch (e) {
      listEmpty = true;
      console.warn(`[setup] ⚠️ 解析设备列表失败，回退 DEVICE_ID=${deviceId}`);
    }
  } else {
    console.warn(`[setup] 拉取设备列表失败（code=${bodyCode(list)}），回退 DEVICE_ID=${deviceId}`);
  }

  // ---------- 3) 设备 Token（DeviceIDAuth：只需 6 位 hex 设备 ID，无需其他凭证） ----------
  let deviceToken = ENV_DEVICE_TOKEN;
  if (deviceToken) {
    console.log('[setup] 使用环境变量 DEVICE_TOKEN 覆盖');
  } else {
    const tok = http.get(`${BASE}/devices/${deviceId}/token`, { tags: { endpoint: 'setup' } });
    if (!bizOk(tok)) {
      fail(
        `[setup] 获取设备 Token 失败，无法继续压测。\n` +
          `  deviceId=${deviceId}  HTTP ${tok.status}  code=${bodyCode(tok)}\n` +
          `  body=${String(tok.body).slice(0, 300)}\n` +
          `  排查：deviceId 是否为已注册的 6 位 hex 设备 ID（可用 DEVICE_ID 覆盖）。`
      );
    }
    try {
      deviceToken = tok.json('data').deviceToken;
    } catch (e) {
      deviceToken = '';
    }
    if (!deviceToken) {
      fail(`[setup] 设备 Token 响应缺少 data.deviceToken，body=${String(tok.body).slice(0, 300)}`);
    }
    console.log(`[setup] 设备 Token 获取成功 deviceToken=${mask(deviceToken)}`);
  }

  // ---------- 4) 端到端预校验：确认两个 Token 真的能用 ----------
  if (!SKIP_SETUP_CHECK) {
    // 4.0 账号必须名下有设备，否则 userTraffic 的设备详情/更新会 403
    if (listEmpty) {
      fail(
        `[setup] 账号 "${USER_ACCOUNT}" 名下没有任何设备，userTraffic 场景无法通过归属校验。\n` +
          `  继续跑会出现：detail ok 100% 失败（HTTP 200 + code:403 无权查看该设备），\n` +
          `  报告再次变成"业务失败"假象 —— 这正是本次修复要避免的情况。\n` +
          `\n  请用**拥有设备的真实账号**运行：\n` +
          `    USER_ACCOUNT=<你的账号> USER_PASSWD=<你的密码> k6 run scripts/k6-bench.js\n` +
          `\n  若只想压设备侧（report/ping/cmd），可加 SKIP_SETUP_CHECK=1 跳过本校验。`
      );
    }

    // 4.1 用户 Token：isLogin 必须为 true
    const isLogin = http.get(`${BASE}/users/isLogin`, {
      headers: { Authorization: userToken },
      tags: { endpoint: 'setup' },
    });
    let loggedIn = false;
    if (bizOk(isLogin)) {
      try {
        loggedIn = isLogin.json('data').isLogin === true;
      } catch (e) {
        loggedIn = false;
      }
    }
    if (!loggedIn) {
      fail(
        `[setup] 用户 Token 预校验失败（isLogin != true）。\n` +
          `  HTTP ${isLogin.status}  code=${bodyCode(isLogin)}  body=${String(isLogin.body).slice(0, 300)}\n` +
          `  说明 Token 无效 —— 压测已中止，避免得到一份全是业务失败的报告。`
      );
    }

    // 4.2 设备 Token：命令拉取接口走 DeviceAuth，用它验证
    const devCheck = http.get(`${BASE}/devices/${deviceId}/commands`, {
      headers: { 'X-Device-Token': deviceToken },
      tags: { endpoint: 'setup' },
    });
    if (!bizOk(devCheck)) {
      fail(
        `[setup] 设备 Token 预校验失败（/commands 返回 code=${bodyCode(devCheck)}）。\n` +
          `  HTTP ${devCheck.status}  body=${String(devCheck.body).slice(0, 300)}\n` +
          `  排查：设备 Token 可能已被轮换或清理，重新调用 /devices/${deviceId}/token 即可。`
      );
    }

    console.log('[setup] ✅ 双 Token 预校验通过，开始压测');
  }

  return { userToken, deviceToken, deviceId };
}

function deviceHeaders(data) {
  return { 'Content-Type': 'application/json', 'X-Device-Token': data.deviceToken };
}

function userHeaders(data) {
  return { Authorization: data.userToken };
}

// ---------- 场景 1：设备侧流量 ----------
// 真实设备周期性上报 + 心跳，有下行需求的设备还会轮询命令
export function deviceTraffic(data) {
  const did = data.deviceId;

  // sensors[].type 为必填（internal/model/sensor_data.go binding:"required"）
  const payload = JSON.stringify({
    sensors: [
      { name: 'temperature', type: 'temperature', value: 25 + Math.random() * 10 },
      { name: 'humidity', type: 'humidity', value: 50 + Math.random() * 20 },
    ],
  });

  const report = http.post(`${BASE}/devices/${did}/sensorData`, payload, {
    headers: deviceHeaders(data),
    tags: { endpoint: 'report' },
  });
  checkBiz(report, 'report ok');

  const ping = http.post(`${BASE}/devices/${did}/heartbeat`, '{}', {
    headers: deviceHeaders(data),
    tags: { endpoint: 'ping' },
  });
  checkBiz(ping, 'ping ok');

  // 20% 的设备轮询下行命令
  if (Math.random() < 0.2) {
    const cmd = http.get(`${BASE}/devices/${did}/commands`, {
      headers: { 'X-Device-Token': data.deviceToken },
      tags: { endpoint: 'cmd' },
    });
    checkBiz(cmd, 'cmd ok');
  }

  sleep(0.4 + Math.random() * 0.4);
}

// ---------- 场景 2：App 用户浏览 ----------
// 打开App → 设备列表 → 点进设备 → 查看历史曲线 → 偶尔更新设备信息
export function userTraffic(data) {
  const hdrs = userHeaders(data);

  const islogin = http.get(`${BASE}/users/isLogin`, {
    headers: hdrs,
    tags: { endpoint: 'islogin' },
  });
  // 不能只看 HTTP 200：token 失效时服务端照样返回 200 + {isLogin:false}
  let loginStateOk = false;
  if (bizOk(islogin)) {
    try {
      loginStateOk = islogin.json('data').isLogin === true;
    } catch (e) {
      // ignore
    }
  }
  bizFail.add(!loginStateOk);
  check(islogin, { 'islogin ok': () => loginStateOk });

  const list = http.get(`${BASE}/devices`, {
    headers: hdrs,
    tags: { endpoint: 'list' },
  });
  const listOk = checkBiz(list, 'list ok');

  // 从列表里取真实设备（保证归属校验通过），取不到才退回 setup 里的 deviceId
  let did = data.deviceId;
  if (listOk) {
    try {
      const arr = list.json('data');
      if (Array.isArray(arr) && arr.length > 0 && arr[0].deviceId) {
        did = arr[0].deviceId;
      }
    } catch (e) {
      // ignore
    }
  }

  const detail = http.get(`${BASE}/devices/${did}`, {
    headers: hdrs,
    tags: { endpoint: 'detail' },
  });
  checkBiz(detail, 'detail ok');

  // 历史曲线（InfluxDB + 15s Redis 缓存；不传 start/end 走默认 3 天，缓存键稳定）
  const history = http.get(`${BASE}/devices/${did}/sensorData?limit=50`, {
    headers: hdrs,
    tags: { endpoint: 'history' },
  });
  checkBiz(history, 'history ok');

  // 10% 的用户会更新设备信息（低频写操作；did 来自列表，保证归属校验通过）
  if (listOk && Math.random() < 0.1) {
    const updatePayload = JSON.stringify({
      deviceName: `ESP32-${String(did).slice(0, 4)}`,
      deviceType: 'esp32',
      firmwareVersion: '1.0.0',
      ipAddress: '192.168.1.10',
      macAddress: 'AA:BB:CC:DD:EE:FF',
      location: '客厅',
    });
    const upd = http.post(`${BASE}/devices/${did}/update`, updatePayload, {
      headers: { ...hdrs, 'Content-Type': 'application/json' },
      tags: { endpoint: 'update' },
    });
    checkBiz(upd, 'update ok');
  }

  // 30% 的用户会看个人资料
  if (Math.random() < 0.3) {
    const profile = http.get(`${BASE}/users/me`, {
      headers: hdrs,
      tags: { endpoint: 'profile' },
    });
    checkBiz(profile, 'profile ok');
  }

  sleep(1 + Math.random());
}

// ---------- 场景 3：WebSocket 长连接 ----------
// App 前台保持 /api/ws/user 连接，收设备状态/命令回执推送
export function wsTraffic(data) {
  const url = `${WS_BASE}/ws/user?token=${encodeURIComponent(data.userToken)}`;
  const res = ws.connect(
    url,
    { tags: { endpoint: 'ws' } },
    (socket) => {
      socket.on('open', () => {
        // 保持连接 3~5 秒后主动关闭，模拟页面停留
        socket.setTimeout(() => socket.close(), 3000 + Math.random() * 2000);
      });
    }
  );
  check(res, { 'ws upgrade 101': (r) => r && r.status === 101 });
  sleep(1);
}

// ---------- 场景 4：登录/登出 ----------
// 登录立即登出，会话数净增为 0，不影响其他场景的 USER_TOKEN
//
// ⚠️ 这里必须用**独立 device 标识**（k6-bench-authflow）：
//    服务端 MaxLoginCount=5 且同端互斥，若与 setup 用同一个 device，
//    反复登录会把主会话 Token 顶掉，导致 userTraffic 全量 401。
export function authFlow() {
  if (!USER_ACCOUNT || !USER_PASSWD) {
    sleep(5);
    return;
  }
  const login = http.post(
    `${BASE}/users/login`,
    JSON.stringify({ account: USER_ACCOUNT, passwd: USER_PASSWD, device: 'k6-bench-authflow' }),
    {
      headers: { 'Content-Type': 'application/json' },
      tags: { endpoint: 'login' },
    }
  );
  if (checkBiz(login, 'login ok')) {
    let token = '';
    try {
      token = login.json('data').tokenValue;
    } catch (e) {
      // ignore
    }
    if (token) {
      const logout = http.post(`${BASE}/users/logout`, null, {
        headers: { Authorization: token },
        tags: { endpoint: 'logout' },
      });
      checkBiz(logout, 'logout ok');
    }
  }
  sleep(2 + Math.random() * 2);
}

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
// 用法:
//   k6 run scripts/k6-bench.js                       # 默认 100 VU 打生产（远端自动放宽延迟阈值）
//   VUS=300 k6 run scripts/k6-bench.js               # 加压
//   BASE_URL=http://127.0.0.1:9091/api k6 run scripts/k6-bench.js   # 直连后端（严格阈值）
// ============================================================
import http from 'k6/http';
import { check, sleep } from 'k6';
import { Rate } from 'k6/metrics';
import ws from 'k6/ws';

// ---------- 环境配置 ----------
// 注意：用户 Token 有效期为 3 天，设备 Token 永不过期但被顶号/清理后立即失效；
// 失效时用环境变量注入新值即可，无需改文件：
//   USER_TOKEN=<新token> DEVICE_TOKEN=<新token> k6 run scripts/k6-bench.js
const BASE = __ENV.BASE_URL || 'https://api.meatsuger.top/api';
const WS_BASE = BASE.replace(/^http/, 'ws');
const DEVICE_ID = __ENV.DEVICE_ID || '369c04';
const DEVICE_TOKEN =
  __ENV.DEVICE_TOKEN || 'c6009184-230b-421d-bd94-cbae8c99c599';
const USER_TOKEN =
  __ENV.USER_TOKEN || 'e7e6da33-64dd-49bd-b973-f6bdc332a2fc';
const USER_ACCOUNT = __ENV.USER_ACCOUNT || 'test';
const USER_PASSWD = __ENV.USER_PASSWD || '123456';
const VUS = parseInt(__ENV.VUS || '100', 10);

// 远端目标要叠加公网 RTT（实测中位 ~86ms），本地直连才用严格 SLO
const IS_LOCAL = /127\.0\.0\.1|localhost/.test(BASE);
const NET = IS_LOCAL ? 0 : 250;

// ---------- 后端处理时间预算（不含网络 RTT，单位 ms） ----------
// 参考网络基准：
//   - apitiming.com（2026）：<100ms 即时、100-200ms 良好、200-500ms 可接受、>1000ms 需优化
//   - 简单端点 p95 < 200ms、复杂查询 p95 < 500ms
//   - Google RAIL（web.dev）：0-100ms 即时、100-1000ms 自然、>1000ms 失焦
//   - Jeff Dean latency numbers：内存 100ns、SSD 随机读 150µs、同机房 RTT 500µs、磁盘寻道 10ms
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

// 带业务失败率埋点的 check（喂给 biz_req_failed 阈值）
function checkBiz(r, label) {
  const ok = bizOk(r);
  bizFail.add(!ok);
  check(r, { [label]: () => ok });
  return ok;
}

function deviceHeaders() {
  return { 'Content-Type': 'application/json', 'X-Device-Token': DEVICE_TOKEN };
}

function userHeaders() {
  return { Authorization: USER_TOKEN };
}

// ---------- 场景 1：设备侧流量 ----------
// 真实设备周期性上报 + 心跳，有下行需求的设备还会轮询命令
export function deviceTraffic() {
  // sensors[].type 为必填（internal/model/sensor_data.go binding:"required"）
  const payload = JSON.stringify({
    sensors: [
      { name: 'temperature', type: 'temperature', value: 25 + Math.random() * 10 },
      { name: 'humidity', type: 'humidity', value: 50 + Math.random() * 20 },
    ],
  });

  const report = http.post(`${BASE}/devices/${DEVICE_ID}/sensorData`, payload, {
    headers: deviceHeaders(),
    tags: { endpoint: 'report' },
  });
  checkBiz(report, 'report ok');

  const ping = http.post(`${BASE}/devices/${DEVICE_ID}/heartbeat`, '{}', {
    headers: deviceHeaders(),
    tags: { endpoint: 'ping' },
  });
  checkBiz(ping, 'ping ok');

  // 20% 的设备轮询下行命令
  if (Math.random() < 0.2) {
    const cmd = http.get(`${BASE}/devices/${DEVICE_ID}/commands`, {
      headers: { 'X-Device-Token': DEVICE_TOKEN },
      tags: { endpoint: 'cmd' },
    });
    checkBiz(cmd, 'cmd ok');
  }

  sleep(0.4 + Math.random() * 0.4);
}

// ---------- 场景 2：App 用户浏览 ----------
// 打开App → 设备列表 → 点进设备 → 查看历史曲线 → 偶尔更新设备信息
export function userTraffic() {
  const hdrs = userHeaders();

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

  // 从列表里取真实设备（保证归属校验通过），取不到才退回 DEVICE_ID
  let did = DEVICE_ID;
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
      deviceName: `ESP32-${did.slice(0, 4)}`,
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
export function wsTraffic() {
  const url = `${WS_BASE}/ws/user?token=${encodeURIComponent(USER_TOKEN)}`;
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
export function authFlow() {
  if (!USER_ACCOUNT || !USER_PASSWD) {
    sleep(5);
    return;
  }
  const login = http.post(
    `${BASE}/users/login`,
    JSON.stringify({ account: USER_ACCOUNT, passwd: USER_PASSWD }),
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

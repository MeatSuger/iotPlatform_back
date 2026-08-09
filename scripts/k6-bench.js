import http from 'k6/http';
import { check, sleep } from 'k6';

// ============================================
// 精确测试：分别测量每个端点的真实延迟
// 用法: k6 run k6-bench.js --vus 200
// ============================================

const BASE = __ENV.BASE_URL || 'http://localhost:8182/api';
// const BASE = __ENV.BASE_URL || 'https://api.meatsuger.top/api';
const DEVICE_ID = __ENV.DEVICE_ID || '369c04';
const TOKEN = __ENV.DEVICE_TOKEN || '3d2175f9-99d7-47c0-a632-9e1ee0e98618';
const USER_TOKEN = __ENV.USER_TOKEN || '2cbe6aa5-607f-4f79-91c9-7533b3132e30';
const VUS = parseInt(__ENV.VUS) || 100;

export const options = {
  stages: [
    { duration: '30s', target: VUS },
    { duration: '1m', target: VUS },
    { duration: '30s', target: 0 },
  ],
  thresholds: {
    // 数据上报 p95 < 50ms
    'http_req_duration{endpoint:data}': ['p(95)<50'],
    // 心跳 p95 < 50ms
    'http_req_duration{endpoint:ping}': ['p(95)<50'],
    // 设备列表 p95 < 500ms（含DB查询）
    'http_req_duration{endpoint:list}': ['p(95)<500'],
    http_req_failed: ['rate<0.05'],
  },
  summaryTrendStats: ['avg', 'min', 'med', 'max', 'p(90)', 'p(95)', 'p(99)'],
};

export default function () {
  if (!TOKEN) return;

  // 1. 数据上报（核心热点路径）
  const dataRes = http.post(`${BASE}/data/${DEVICE_ID}/Data`,
    JSON.stringify({
      sensors: [
        { name: 'temperature', value: 25 + Math.random() * 10 },
        { name: 'humidity', value: 50 + Math.random() * 20 },
      ],
    }),
    {
      headers: { 'Content-Type': 'application/json', 'X-Device-Token': TOKEN },
      tags: { endpoint: 'data' },
    }
  );
  check(dataRes, { 'data:200': (r) => r.status === 200 });

  // 2. 心跳
  const pingRes = http.post(`${BASE}/data/${DEVICE_ID}/ping`, '{}', {
    headers: { 'Content-Type': 'application/json', 'X-Device-Token': TOKEN },
    tags: { endpoint: 'ping' },
  });
  check(pingRes, { 'ping:200': (r) => r.status === 200 });

  // 3. 设备列表（较慢，含 DB 查询）
  if (USER_TOKEN) {
    const listRes = http.get(`${BASE}/device/list`, {
      headers: { 'Authorization': USER_TOKEN },
      tags: { endpoint: 'list' },
    });
    check(listRes, { 'list:200': (r) => r.status === 200 });
  }

  sleep(0.1);
}

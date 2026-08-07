import http from 'k6/http';
import {check, sleep} from 'k6';

// ============================================
// IoT Platform 压力测试
// 用法: k6 run k6-test.js
// ============================================

// const BASE = __ENV.BASE_URL || 'https://api.meatsuger.top/api';
// const BASE = __ENV.BASE_URL || 'http://47.99.74.160:9091/api';
const BASE = __ENV.BASE_URL || 'http://localhost:8182/api';
const VUS = __ENV.VUS || 10;

export const options = {
  // 1. 负载阶段
  stages: [
    { duration: '30s', target: VUS },
    { duration: '1m', target: VUS },
    { duration: '30s', target: 0 },
  ],

  // 2. 全局 HTTP 超时
  http: {
    timeout: '5s',
  },

  // 3. 多维度阈值
  thresholds: {
    // 全局性能底线
    http_req_duration: ['p(95)<500'],

    // 错误率
    http_req_failed: ['rate<0.05'],

    // 保证吞吐量（假设你的服务至少能处理 50 rps）
    http_reqs: ['rate > 50'],
  },

  // 4. 摘要中显示更多统计维度（可选）
  summaryTrendStats: ['avg', 'min', 'med', 'max', 'p(90)', 'p(95)', 'p(99)'],
};

// 测试设备
const DEVICE_ID = __ENV.DEVICE_ID || '369c04';
const TOKEN     = __ENV.DEVICE_TOKEN || '3d2175f9-99d7-47c0-a632-9e1ee0e98618';
const USER_TOKEN     = __ENV.USER_TOKEN || '2cbe6aa5-607f-4f79-91c9-7533b3132e30';

export default function () {
  // ===== 2. 设备数据上报 =====
  if (TOKEN) {
    const dataRes = http.post(`${BASE}/data/${DEVICE_ID}/Data`, JSON.stringify({
      type: 'data',
      sensors: [
        { name: 'temperature', value: 25 + Math.random() * 10 },
        { name: 'humidity',    value: 50 + Math.random() * 20 },
      ],
    }), {
      headers: {
        'Content-Type':    'application/json',
        'X-Device-Token':  TOKEN,
      },
    });

    check(dataRes, { 'report: 200': (r) => r.status === 200 });

    // ===== 3. 心跳 =====
    const pingRes = http.post(`${BASE}/data/${DEVICE_ID}/ping`, '{}', {
      headers: {
        'Content-Type':    'application/json',
        'X-Device-Token':  TOKEN,
      },
    });

    check(pingRes, { 'ping: 200': (r) => r.status === 200 });

    const listRes = http.get(`${BASE}/device/list`,{
      headers:{
        'Authorization': USER_TOKEN,
      }
    });
    check(listRes, { 'report: 200': (r) => r.status === 200 });
  }

  sleep(1);
}

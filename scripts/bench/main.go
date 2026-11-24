// 物联网设备接入与物模型管理平台软件（物咸通）V1.0
// Copyright (C) 2025-2026 余昊
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

// bench 服务端本机压测器（配合 scripts/bench.sh 使用）。
//
// 为什么需要它：从家宽/热点跑 k6 时，363 并发新建连接里 37% 会被本地 NAT 丢掉
// （实测 400 并发 150 个 dial timeout），k6 报的 p95/错误率量的是自家宽带，
// 不是服务端。这个工具在服务器上直接打 127.0.0.1，隔离链路，量服务端真实能力。
package main

import (
	"flag"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

func main() {
	base := flag.String("base", "http://127.0.0.1:9091/api", "API base")
	account := flag.String("account", "meatsuger", "user account")
	passwd := flag.String("passwd", "aaaaaa", "user password")
	deviceID := flag.String("device", "adc0c5", "device id for read endpoints")
	deviceToken := flag.String("device-token", "", "device token (空则跳过写接口)")
	conc := flag.Int("c", 100, "并发")
	dur := flag.Duration("d", 15*time.Second, "每个接口的压测时长")
	flag.Parse()

	token, err := login(*base, *account, *passwd)
	if err != nil {
		fmt.Println("登录失败:", err)
		return
	}

	type ep struct {
		name, method, path, header, tok string
	}
	eps := []ep{
		{"islogin", "GET", "/users/isLogin", "Authorization", token},
		{"list", "GET", "/devices", "Authorization", token},
		{"detail", "GET", "/devices/" + *deviceID, "Authorization", token},
		{"history", "GET", "/devices/" + *deviceID + "/sensorData?limit=50", "Authorization", token},
		// 设备接口只认 X-Device-Token（见 middleware.extractDeviceToken）
		{"report", "POST", "/devices/" + *deviceID + "/sensorData", "X-Device-Token", *deviceToken},
		{"ping", "POST", "/devices/" + *deviceID + "/heartbeat", "X-Device-Token", *deviceToken},
	}

	fmt.Printf("%-10s %9s %7s %9s %9s %9s %9s %9s\n", "endpoint", "rps", "n", "err", "p50", "p90", "p95", "p99")
	for _, e := range eps {
		if e.tok == "" {
			continue // 没有相应用 token（如未传 -device-token）就跳过
		}
		res := run(*base+e.path, e.method, e.header, e.tok, *conc, *dur)
		fmt.Printf("%-10s %9.0f %7d %9d %9v %9v %9v %9v\n",
			e.name, res.rps, res.n, res.err, res.p50, res.p90, res.p95, res.p99)
	}
}

func login(base, account, passwd string) (string, error) {
	url := fmt.Sprintf("%s/users/login?account=%s&passwd=%s", base, account, passwd)
	resp, err := http.Post(url, "application/json", nil)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	const key = `"tokenValue":"`
	i := strings.Index(string(body), key)
	if i < 0 {
		return "", fmt.Errorf("响应里没有 tokenValue: %s", truncate(string(body), 120))
	}
	rest := string(body)[i+len(key):]
	j := strings.IndexByte(rest, '"')
	if j < 0 {
		return "", fmt.Errorf("tokenValue 格式异常")
	}
	return rest[:j], nil
}

type result struct {
	rps                float64
	n, err             int64
	p50, p90, p95, p99 time.Duration
}

func run(url, method, header, token string, conc int, dur time.Duration) result {
	tr := &http.Transport{MaxIdleConnsPerHost: conc * 2, MaxIdleConns: conc * 2}
	client := &http.Client{Transport: tr, Timeout: 30 * time.Second}

	body := ""
	if method == "POST" {
		body = `{"sensors":[{"name":"temperature","type":"number","value":25.5}]}`
	}

	var mu sync.Mutex
	var lat []time.Duration
	var n, errs int64
	stop := time.Now().Add(dur)

	var wg sync.WaitGroup
	for range conc {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for time.Now().Before(stop) {
				var rd io.Reader
				if body != "" {
					rd = strings.NewReader(body)
				}
				req, rerr := http.NewRequest(method, url, rd)
				if rerr != nil {
					continue
				}
				if token != "" {
					req.Header.Set(header, token)
				}
				if body != "" {
					req.Header.Set("Content-Type", "application/json")
				}
				t0 := time.Now()
				resp, derr := client.Do(req)
				d := time.Since(t0)
				atomic.AddInt64(&n, 1)
				if derr != nil {
					atomic.AddInt64(&errs, 1)
					continue
				}
				_, _ = io.Copy(io.Discard, resp.Body)
				_ = resp.Body.Close()
				if resp.StatusCode >= 400 {
					atomic.AddInt64(&errs, 1)
					continue
				}
				mu.Lock()
				lat = append(lat, d)
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	sort.Slice(lat, func(i, j int) bool { return lat[i] < lat[j] })
	q := func(f float64) time.Duration {
		if len(lat) == 0 {
			return 0
		}
		return lat[int(float64(len(lat)-1)*f)].Round(time.Millisecond)
	}
	return result{
		rps: float64(n) / dur.Seconds(),
		n:   n, err: errs,
		p50: q(0.5), p90: q(0.9), p95: q(0.95), p99: q(0.99),
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

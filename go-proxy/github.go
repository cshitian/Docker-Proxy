package main

import (
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// GitHub 前缀代理（二次开发新增）：
// 请求形如 /https://github.com/<owner>/<repo>/releases/download/... 或
// /https://api.github.com/repos/<owner>/<repo>/releases/latest 时，
// 把路径中嵌入的完整网址当作上游透明转发，用于 GitHub 发版文件下载
// 与 API 请求加速；其余请求（注册表 /v2/ 等）不进此分支，原有逻辑不变。
//
// 安全口径：
//   - 只放行 GitHub 系域名（githubAllowedHosts 与 *.githubusercontent.com）；
//   - 只允许 GET / HEAD；
//   - 本分支挂在 ServeHTTP 的 IP 访问控制之后，复用既有 ACL 名单管控来源。
var githubAllowedHosts = map[string]bool{
	"github.com":                true,
	"api.github.com":            true,
	"codeload.github.com":       true,
	"gist.github.com":           true,
	"uploads.github.com":        true,
	"raw.githubusercontent.com": true,
}

func githubHostAllowed(host string) bool {
	host = strings.ToLower(host)
	if githubAllowedHosts[host] {
		return true
	}
	// objects.githubusercontent.com / media.githubusercontent.com 等资产域名
	return strings.HasSuffix(host, ".githubusercontent.com")
}

// githubRelayTarget 从请求路径提取要转发的完整网址。
// 仅当前缀形式命中时返回 ok=true；普通注册表路径返回 false。
func githubRelayTarget(u *url.URL) (string, bool) {
	p := u.EscapedPath()
	if !strings.HasPrefix(p, "/http://") && !strings.HasPrefix(p, "/https://") {
		// 个别客户端会把 // 压成 /，修复后再判断
		if strings.HasPrefix(p, "/http:/") || strings.HasPrefix(p, "/https:/") {
			p = strings.Replace(p, ":/", "://", 1)
		} else {
			return "", false
		}
	}
	target := strings.TrimPrefix(p, "/")
	if u.RawQuery != "" {
		target += "?" + u.RawQuery
	}
	return target, true
}

var githubRelayClient = &http.Client{
	Transport: &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   15 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSHandshakeTimeout:   15 * time.Second,
		ResponseHeaderTimeout: 120 * time.Second,
	},
	// 默认跟随重定向：api.github.com 的资产下载会 302 到
	// objects.githubusercontent.com 的签名地址，在本机跟随完成，
	// 客户端无需再直连 GitHub 的资产域名。
}

var hopByHopHeaders = map[string]bool{
	"Connection":          true,
	"Keep-Alive":          true,
	"Proxy-Authenticate":  true,
	"Proxy-Authorization": true,
	"Te":                  true,
	"Trailer":             true,
	"Transfer-Encoding":   true,
	"Upgrade":             true,
}

func (p *Proxy) serveGitHubRelay(w http.ResponseWriter, r *http.Request, target string) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	u, err := url.Parse(target)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") {
		http.Error(w, "invalid target url", http.StatusBadRequest)
		return
	}
	if !githubHostAllowed(u.Hostname()) {
		log.Printf("[github] denied host %s", u.Host)
		http.Error(w, "host not allowed", http.StatusForbidden)
		return
	}
	req, err := http.NewRequestWithContext(r.Context(), r.Method, target, nil)
	if err != nil {
		http.Error(w, "bad gateway", http.StatusBadGateway)
		return
	}
	// 透传请求头（含 Authorization，私有仓库令牌靠它送达 GitHub）。
	for k, vv := range r.Header {
		if hopByHopHeaders[http.CanonicalHeaderKey(k)] {
			continue
		}
		for _, v := range vv {
			req.Header.Add(k, v)
		}
	}
	resp, err := githubRelayClient.Do(req)
	if err != nil {
		log.Printf("[github] %s %s failed: %v", r.Method, target, err)
		http.Error(w, "github upstream error", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	for k, vv := range resp.Header {
		if hopByHopHeaders[http.CanonicalHeaderKey(k)] {
			continue
		}
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	if r.Method == http.MethodGet {
		_, _ = io.Copy(w, resp.Body)
	}
	log.Printf("[github] %s %s -> %d", r.Method, target, resp.StatusCode)
}

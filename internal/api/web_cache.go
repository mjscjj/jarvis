package api

import (
	"context"
	"strings"

	"github.com/cloudwego/hertz/pkg/app"
)

// WebEntryNoStore 阻止 webview 缓存前端入口 HTML。
//
// Hertz 的静态文件服务只发 Last-Modified，不发 Cache-Control，WKWebView 于是按
// 启发式规则把 index.html 缓存下来。桌面应用升级后，新入口会引用新的带哈希
// chunk，旧 chunk 同时被 appservice 的资产同步清理掉；此时 webview 仍在执行缓存
// 里的旧 index.html，去请求已经不存在的文件，动态导入失败导致整个界面白屏。
//
// 入口文档必须每次回源，/assets/* 靠文件名哈希天然可缓存，不在此列。
func WebEntryNoStore() app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		c.Next(ctx)
		if isWebEntryDocument(string(c.Request.Path())) {
			c.Response.Header.Set("Cache-Control", "no-store")
		}
	}
}

func isWebEntryDocument(path string) bool {
	if strings.HasPrefix(path, "/api/") || strings.HasPrefix(path, UpdateFilePrefix) {
		return false
	}
	return path == "/" || strings.HasSuffix(path, ".html")
}

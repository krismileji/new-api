package router

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCoderProxyPublicSEOResponses(t *testing.T) {
	previousAddress := system_setting.ServerAddress
	system_setting.ServerAddress = siteSEOOrigin
	t.Cleanup(func() { system_setting.ServerAddress = previousAddress })
	legal := system_setting.GetLegalSettings()
	previousAgreement, previousPrivacy := legal.UserAgreement, legal.PrivacyPolicy
	legal.UserAgreement, legal.PrivacyPolicy = "", ""
	t.Cleanup(func() { legal.UserAgreement, legal.PrivacyPolicy = previousAgreement, previousPrivacy })
	common.OptionMapRWMutex.Lock()
	previousOptionMap := common.OptionMap
	if previousOptionMap == nil {
		common.OptionMap = make(map[string]string)
	}
	previousHome := common.OptionMap["HomePageContent"]
	previousAbout := common.OptionMap["About"]
	common.OptionMap["HomePageContent"] = ""
	common.OptionMap["About"] = ""
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		if previousOptionMap == nil {
			common.OptionMap = nil
		} else {
			common.OptionMap["HomePageContent"] = previousHome
			common.OptionMap["About"] = previousAbout
		}
		common.OptionMapRWMutex.Unlock()
	})

	index := `<!doctype html><html lang="en"><head><title>New API</title><meta name="description" content="Unified AI API gateway and admin dashboard."></head><body><div id="root"></div></body></html>`
	outer := gin.New()
	SetWebRouter(outer, WebAssets{IndexPage: []byte(index)}, func(c *gin.Context) { c.Next() })

	english := performPluginRequest(outer, http.MethodGet, "/")
	require.Equal(t, http.StatusOK, english.Code)
	assert.Contains(t, english.Body.String(), "<h1>One gateway for AI model APIs</h1>")
	assert.Contains(t, english.Body.String(), `rel="canonical" href="https://coder.work-proxy.com/"`)
	assert.Contains(t, english.Body.String(), `hreflang="zh-CN"`)
	assert.Contains(t, english.Body.String(), `hreflang="x-default" href="https://coder.work-proxy.com/"`)
	assert.Contains(t, english.Body.String(), `application/ld+json`)
	assert.Contains(t, english.Body.String(), `id="root"`)
	assert.Contains(t, english.Body.String(), `<title>New API</title>`)
	assert.Contains(t, english.Body.String(), `content="Unified AI API gateway and admin dashboard."`)
	assert.Equal(t, "no-cache, no-transform", english.Header().Get("Cache-Control"))
	assert.Contains(t, english.Body.String(), `mailto:krismiledev@gmail.com`)
	assert.NotContains(t, english.Body.String(), `href="/privacy-policy"`)

	chinese := performPluginRequest(outer, http.MethodGet, "/?lang=zh-CN")
	require.Equal(t, http.StatusOK, chinese.Code)
	assert.Contains(t, chinese.Body.String(), `lang="zh-CN"`)
	assert.Contains(t, chinese.Body.String(), "一个入口，连接多种 AI 模型 API")
	assert.Contains(t, chinese.Body.String(), `href="https://coder.work-proxy.com/?lang=zh-CN"`)
	assert.Contains(t, chinese.Body.String(), `content="Unified AI API gateway and admin dashboard."`)
	assert.Contains(t, chinese.Body.String(), `hreflang="x-default" href="https://coder.work-proxy.com/"`)

	about := performPluginRequest(outer, http.MethodGet, "/about")
	assert.Equal(t, http.StatusOK, about.Code)
	assert.Contains(t, about.Body.String(), "About Coder Proxy")
	assert.NotContains(t, about.Body.String(), `id="root"`)
	contact := performPluginRequest(outer, http.MethodGet, "/contact?lang=zh-CN")
	assert.Equal(t, http.StatusOK, contact.Code)
	assert.Contains(t, contact.Body.String(), "krismiledev@gmail.com")
	assert.Contains(t, contact.Body.String(), "联系 Coder Proxy")
	for _, path := range []string{"/privacy-policy", "/user-agreement"} {
		missingLegal := performPluginRequest(outer, http.MethodGet, path)
		assert.Equal(t, http.StatusNotFound, missingLegal.Code)
		assert.Equal(t, "no-store", missingLegal.Header().Get("Cache-Control"))
	}
	legal.UserAgreement = "Coder API 用户协议\n\n**生效日期：2026 年 11 月 04 日**\n\n## 服务范围\n\n已发布的协议内容。"
	legal.PrivacyPolicy = "Coder API 隐私政策\n\n**生效日期：2026 年 11 月 04 日**\n\n## 数据处理\n\n已发布的政策内容。"
	legalHome := performPluginRequest(outer, http.MethodGet, "/?lang=zh-CN")
	assert.Contains(t, legalHome.Body.String(), `href="/privacy-policy">隐私政策</a>`)
	assert.Contains(t, legalHome.Body.String(), `href="/user-agreement">用户协议</a>`)
	for _, page := range []struct{ path, heading, section, content string }{
		{"/privacy-policy", "隐私政策", "数据处理", "已发布的政策内容。"},
		{"/user-agreement", "用户协议", "服务范围", "已发布的协议内容。"},
	} {
		publishedLegal := performPluginRequest(outer, http.MethodGet, page.path)
		assert.Equal(t, http.StatusOK, publishedLegal.Code)
		assert.Contains(t, publishedLegal.Body.String(), "<h1>"+page.heading+"</h1>")
		assert.Contains(t, publishedLegal.Body.String(), "<h2>"+page.section+"</h2>")
		assert.Contains(t, publishedLegal.Body.String(), page.content)
		assert.Contains(t, publishedLegal.Body.String(), `rel="canonical" href="https://coder.work-proxy.com`+page.path+`"`)
		assert.NotContains(t, publishedLegal.Body.String(), `hreflang="en"`)
		assert.Equal(t, "no-cache, no-transform", publishedLegal.Header().Get("Cache-Control"))
	}
	legal.PrivacyPolicy = "## 安全内容\n\n<script>alert(1)</script>\n\n[危险链接](javascript:alert(1))"
	unsafeLegal := performPluginRequest(outer, http.MethodGet, "/privacy-policy")
	assert.NotContains(t, unsafeLegal.Body.String(), "<script>alert(1)</script>")
	assert.NotContains(t, unsafeLegal.Body.String(), `href="javascript:`)
	legal.PrivacyPolicy = "Coder API 隐私政策\n\n## 数据处理\n\n已发布的政策内容。"

	robots := performPluginRequest(outer, http.MethodGet, "/robots.txt")
	assert.Equal(t, http.StatusOK, robots.Code)
	assert.Contains(t, robots.Header().Get("Content-Type"), "text/plain")
	assert.Contains(t, robots.Body.String(), "Sitemap: https://coder.work-proxy.com/sitemap.xml")
	assert.NotContains(t, robots.Body.String(), "<!doctype html>")
	llms := performPluginRequest(outer, http.MethodGet, "/llms.txt")
	assert.Equal(t, http.StatusOK, llms.Code)
	assert.Contains(t, llms.Body.String(), "## 页面")
	assert.Contains(t, llms.Body.String(), "- [隐私政策](https://coder.work-proxy.com/privacy-policy)")
	missingFile := performPluginRequest(outer, http.MethodGet, "/unknown.xml")
	assert.Equal(t, http.StatusNotFound, missingFile.Code)
	for _, path := range []string{"/seo-nonexistent-page", "/about/missing", "/pricing/missing/extra"} {
		missingPage := performPluginRequest(outer, http.MethodGet, path)
		assert.Equal(t, http.StatusNotFound, missingPage.Code)
		assert.Equal(t, "no-store", missingPage.Header().Get("Cache-Control"))
	}
	for _, path := range []string{"/pricing", "/pricing/model-id", "/security", "/system-settings/site/system-info", "/console/token"} {
		appPage := performPluginRequest(outer, http.MethodGet, path)
		assert.Equal(t, http.StatusOK, appPage.Code)
		assert.Equal(t, index, appPage.Body.String())
	}
	sitemap := performPluginRequest(outer, http.MethodGet, "/sitemap.xml")
	assert.Equal(t, http.StatusOK, sitemap.Code)
	assert.Contains(t, sitemap.Header().Get("Content-Type"), "application/xml")
	assert.Contains(t, sitemap.Body.String(), "<loc>https://coder.work-proxy.com/?lang=zh-CN</loc>")
	assert.Contains(t, sitemap.Body.String(), "<loc>https://coder.work-proxy.com/contact</loc>")
	assert.Contains(t, sitemap.Body.String(), "<loc>https://coder.work-proxy.com/privacy-policy</loc>")
	assert.Contains(t, sitemap.Body.String(), "<loc>https://coder.work-proxy.com/user-agreement</loc>")
	legal.PrivacyPolicy = "https://example.com/privacy"
	externalLegal := performPluginRequest(outer, http.MethodGet, "/privacy-policy")
	assert.Equal(t, index, externalLegal.Body.String())
	externalSitemap := performPluginRequest(outer, http.MethodGet, "/sitemap.xml")
	assert.NotContains(t, externalSitemap.Body.String(), "<loc>https://coder.work-proxy.com/privacy-policy</loc>")
	externalLlms := performPluginRequest(outer, http.MethodGet, "/llms.txt")
	assert.NotContains(t, externalLlms.Body.String(), "- [隐私政策]")
	legal.PrivacyPolicy = "导语 <p>HTML 正文</p>"
	htmlLegal := performPluginRequest(outer, http.MethodGet, "/privacy-policy")
	assert.Equal(t, index, htmlLegal.Body.String())

	common.OptionMapRWMutex.Lock()
	common.OptionMap["HomePageContent"] = "custom homepage"
	common.OptionMap["About"] = "custom about"
	common.OptionMapRWMutex.Unlock()
	customHome := performPluginRequest(outer, http.MethodGet, "/")
	assert.Equal(t, index, customHome.Body.String())
	customAbout := performPluginRequest(outer, http.MethodGet, "/about")
	assert.Equal(t, index, customAbout.Body.String())
	customSitemap := performPluginRequest(outer, http.MethodGet, "/sitemap.xml")
	assert.NotContains(t, customSitemap.Body.String(), "<loc>https://coder.work-proxy.com/</loc>")
	assert.NotContains(t, customSitemap.Body.String(), "<loc>https://coder.work-proxy.com/about</loc>")

	system_setting.ServerAddress = "https://other.example.com"
	otherSiteHome := performPluginRequest(outer, http.MethodGet, "/")
	assert.Equal(t, index, otherSiteHome.Body.String())
}

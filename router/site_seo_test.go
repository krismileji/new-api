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
	assert.Contains(t, english.Body.String(), `application/ld+json`)
	assert.Contains(t, english.Body.String(), `id="root"`)
	assert.Contains(t, english.Body.String(), `<title>New API</title>`)
	assert.Contains(t, english.Body.String(), `content="Unified AI API gateway and admin dashboard."`)

	chinese := performPluginRequest(outer, http.MethodGet, "/?lang=zh-CN")
	require.Equal(t, http.StatusOK, chinese.Code)
	assert.Contains(t, chinese.Body.String(), `lang="zh-CN"`)
	assert.Contains(t, chinese.Body.String(), "一个入口，连接多种 AI 模型 API")
	assert.Contains(t, chinese.Body.String(), `href="https://coder.work-proxy.com/?lang=zh-CN"`)
	assert.Contains(t, chinese.Body.String(), `content="Unified AI API gateway and admin dashboard."`)

	about := performPluginRequest(outer, http.MethodGet, "/about")
	assert.Equal(t, http.StatusOK, about.Code)
	assert.Contains(t, about.Body.String(), "About Coder Proxy")
	assert.NotContains(t, about.Body.String(), `id="root"`)
	contact := performPluginRequest(outer, http.MethodGet, "/contact?lang=zh-CN")
	assert.Equal(t, http.StatusOK, contact.Code)
	assert.Contains(t, contact.Body.String(), "krismiledev@gmail.com")
	assert.Contains(t, contact.Body.String(), "联系 Coder Proxy")

	robots := performPluginRequest(outer, http.MethodGet, "/robots.txt")
	assert.Equal(t, http.StatusOK, robots.Code)
	assert.Contains(t, robots.Header().Get("Content-Type"), "text/plain")
	assert.Contains(t, robots.Body.String(), "Sitemap: https://coder.work-proxy.com/sitemap.xml")
	assert.NotContains(t, robots.Body.String(), "<!doctype html>")
	missingFile := performPluginRequest(outer, http.MethodGet, "/unknown.xml")
	assert.Equal(t, http.StatusNotFound, missingFile.Code)
	sitemap := performPluginRequest(outer, http.MethodGet, "/sitemap.xml")
	assert.Equal(t, http.StatusOK, sitemap.Code)
	assert.Contains(t, sitemap.Header().Get("Content-Type"), "application/xml")
	assert.Contains(t, sitemap.Body.String(), "<loc>https://coder.work-proxy.com/?lang=zh-CN</loc>")
	assert.Contains(t, sitemap.Body.String(), "<loc>https://coder.work-proxy.com/contact</loc>")

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

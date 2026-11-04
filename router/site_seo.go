package router

import (
	"bytes"
	"encoding/xml"
	"html/template"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/yuin/goldmark"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

const siteSEOOrigin = "https://coder.work-proxy.com"

var siteSEOHTMLPattern = regexp.MustCompile(`(?i)<!doctype html|<html[\s>]|<head[\s>]|<body[\s>]|<style[\s>]|<script[\s>]|</?[a-z][\s\S]*>`)

type siteSEOPages struct {
	home    [4]map[string][]byte
	about   [4]map[string][]byte
	contact [4]map[string][]byte
}

type siteSEOCopy struct {
	Lang          string
	OtherLang     string
	OtherLabel    string
	HomeLabel     string
	AboutLabel    string
	ContactLabel  string
	PricingLabel  string
	DocsLabel     string
	SignupLabel   string
	Title         string
	SocialTitle   string
	Description   string
	Heading       string
	Lead          string
	Sections      []siteSEOSection
	ContactIntro  string
	ProjectNote   string
	PagePath      string
	PageKind      string
	ShowAgreement bool
	ShowPrivacy   bool
	LegalContent  template.HTML
}

type siteSEOSection struct {
	Title string
	Text  string
}

const siteSEOStyle = `
  #seo-initial { font-family: system-ui, sans-serif; color: #14212a; background: #f8fafb; min-height: 100vh; }
  #seo-initial * { box-sizing: border-box; }
  #seo-initial a { color: inherit; }
  #seo-initial .seo-wrap { width: min(1100px, calc(100% - 40px)); margin: auto; }
  #seo-initial .seo-nav { display: flex; align-items: center; flex-wrap: wrap; justify-content: space-between; gap: 20px; padding: 24px 0; border-bottom: 1px solid #dce4e8; }
  #seo-initial .seo-brand { display: inline-flex; align-items: center; gap: 10px; font-weight: 750; font-size: 19px; text-decoration: none; }
  #seo-initial .seo-brand img { width: 30px; height: 30px; object-fit: contain; }
  #seo-initial .seo-links { display: flex; flex-wrap: wrap; gap: 18px; align-items: center; font-size: 14px; }
  #seo-initial .seo-links a { text-decoration: none; }
  #seo-initial .seo-links a:hover, #seo-initial footer a:hover { text-decoration: underline; }
  #seo-initial .seo-language { color: #17666e; font-weight: 650; }
  #seo-initial main { padding: 76px 0 94px; }
  #seo-initial .seo-eyebrow { color: #17666e; font-weight: 700; font-size: 13px; margin: 0 0 16px; }
  #seo-initial h1 { font-size: clamp(36px, 5vw, 58px); line-height: 1.12; letter-spacing: 0; margin: 0 0 24px; max-width: 850px; }
  #seo-initial .seo-lead { color: #44535e; font-size: 18px; line-height: 1.8; max-width: 760px; margin: 0 0 28px; }
  #seo-initial .seo-actions { display: flex; flex-wrap: wrap; gap: 12px; margin-bottom: 68px; }
  #seo-initial .seo-actions a { padding: 12px 18px; border: 1px solid #1b6f76; border-radius: 6px; text-decoration: none; font-weight: 650; }
  #seo-initial .seo-actions a:first-child { background: #17666e; color: white; }
  #seo-initial .seo-sections { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 38px; border-top: 1px solid #dce4e8; padding-top: 36px; }
  #seo-initial h2 { font-size: 20px; margin: 0 0 12px; }
  #seo-initial .seo-sections p, #seo-initial .seo-note, #seo-initial .seo-contact p { color: #44535e; line-height: 1.75; margin: 0; }
  #seo-initial .seo-contact { border-top: 1px solid #dce4e8; margin-top: 50px; padding-top: 36px; }
  #seo-initial .seo-contact p + p { margin-top: 10px; }
  #seo-initial .seo-note { margin-top: 54px; font-size: 14px; }
  #seo-initial .seo-legal { max-width: 800px; color: #33434d; line-height: 1.8; overflow-wrap: anywhere; }
  #seo-initial .seo-legal h2, #seo-initial .seo-legal h3 { margin: 32px 0 12px; }
  #seo-initial .seo-legal p, #seo-initial .seo-legal ul, #seo-initial .seo-legal ol { margin: 0 0 16px; }
  #seo-initial footer { border-top: 1px solid #dce4e8; padding: 24px 0 36px; font-size: 13px; color: #596771; }
  @media (max-width: 700px) { #seo-initial main { padding: 48px 0 72px; } #seo-initial .seo-sections { grid-template-columns: 1fr; gap: 28px; } #seo-initial .seo-lead { font-size: 16px; } }
`

var siteSEOBody = template.Must(template.New("site-seo-body").Parse(`
<div id="seo-initial">
  <div class="seo-wrap">
    <header class="seo-nav">
      <a class="seo-brand" href="/{{if eq .Lang "zh-CN"}}?lang=zh-CN{{end}}"><img src="/logo.png" alt="" width="30" height="30">Coder Proxy</a>
      <nav class="seo-links" aria-label="Navigation">
        <a href="/{{if eq .Lang "zh-CN"}}?lang=zh-CN{{end}}">{{.HomeLabel}}</a>
        <a href="/about{{if eq .Lang "zh-CN"}}?lang=zh-CN{{end}}">{{.AboutLabel}}</a>
        <a href="/pricing">{{.PricingLabel}}</a>
        <a href="/contact{{if eq .Lang "zh-CN"}}?lang=zh-CN{{end}}">{{.ContactLabel}}</a>
        {{if .OtherLang}}<a class="seo-language" href="{{.PagePath}}{{if eq .OtherLang "zh-CN"}}?lang=zh-CN{{end}}" lang="{{.OtherLang}}">{{.OtherLabel}}</a>{{end}}
      </nav>
    </header>
    <main>
      <p class="seo-eyebrow">Coder Proxy</p>
      <h1>{{.Heading}}</h1>
      {{if .Lead}}<p class="seo-lead">{{.Lead}}</p>{{end}}
      {{if eq .PageKind "home"}}
      <div class="seo-actions">
        <a href="/sign-up">{{.SignupLabel}}</a>
        <a href="/pricing">{{.PricingLabel}}</a>
        <a href="https://docs.newapi.pro/">{{.DocsLabel}}</a>
      </div>
      {{end}}
      {{if .Sections}}
      <div class="seo-sections">
        {{range .Sections}}<section><h2>{{.Title}}</h2><p>{{.Text}}</p></section>{{end}}
      </div>
      {{end}}
      {{if .LegalContent}}<article class="seo-legal">{{.LegalContent}}</article>{{end}}
      {{if .ContactIntro}}
      <section class="seo-contact" id="contact">
        <h2>{{.ContactLabel}}</h2>
        <p>{{.ContactIntro}}</p>
        <p>Telegram: <a href="https://t.me/krismileji">@krismileji</a></p>
        <p>Email: <a href="mailto:krismiledev@gmail.com">krismiledev@gmail.com</a></p>
      </section>
      {{end}}
      <p class="seo-note">{{.ProjectNote}} <a href="https://github.com/QuantumNous/new-api">New API · QuantumNous</a>.</p>
    </main>
    <footer>Coder Proxy · <a href="/about{{if eq .Lang "zh-CN"}}?lang=zh-CN{{end}}">{{.AboutLabel}}</a> · <a href="/contact{{if eq .Lang "zh-CN"}}?lang=zh-CN{{end}}">{{.ContactLabel}}</a>{{if .ShowAgreement}} · <a href="/user-agreement">用户协议</a>{{end}}{{if .ShowPrivacy}} · <a href="/privacy-policy">隐私政策</a>{{end}} · <a href="https://github.com/QuantumNous/new-api">New API · QuantumNous</a></footer>
  </div>
</div>`))

func siteCopy(kind, lang string) siteSEOCopy {
	copy := siteSEOCopy{Lang: lang, PageKind: kind, PagePath: "/", OtherLang: "zh-CN", OtherLabel: "简体中文", HomeLabel: "Home", AboutLabel: "About", ContactLabel: "Contact", PricingLabel: "Pricing", DocsLabel: "Documentation", SignupLabel: "Get started", ProjectNote: "The gateway software is based on the New API project by"}
	if kind != "home" {
		copy.PagePath = "/" + kind
	}
	if lang == "zh-CN" {
		copy.OtherLang, copy.OtherLabel = "en", "English"
		copy.HomeLabel, copy.AboutLabel, copy.ContactLabel = "首页", "关于我们", "联系我们"
		copy.PricingLabel, copy.DocsLabel, copy.SignupLabel = "查看价格", "使用文档", "开始使用"
		copy.ProjectNote = "本站网关软件基于 New API 项目，原项目及作者："
	}
	switch kind {
	case "home":
		copy.Title = "New API"
		copy.SocialTitle = "Coder Proxy | AI API aggregation platform"
		copy.Description = "Access multiple AI models through Coder Proxy's unified API gateway. Explore pricing, create an account, and connect your applications."
		copy.Heading = "One gateway for AI model APIs"
		copy.Lead = "Coder Proxy is an AI aggregation platform. Use one account to access supported models through a unified API, review current pricing, and manage your usage from one dashboard."
		copy.Sections = []siteSEOSection{{"Access AI models", "Connect applications through supported API protocols and model routes. Available models and capabilities depend on the current service configuration."}, {"Understand costs", "Check the public pricing page before use. Usage and charges can be reviewed in your account dashboard."}, {"Start with documentation", "Create an account, obtain an API key, and follow the integration documentation for your chosen protocol."}}
		copy.ContactIntro = "Questions about access, billing, or integration? Reach us through these channels."
		if lang == "zh-CN" {
			copy.SocialTitle = "Coder Proxy｜AI 模型聚合 API 平台"
			copy.Description = "通过 Coder Proxy 的统一 API 接入多种 AI 模型。查看当前价格、创建账户，并在控制台管理用量。"
			copy.Heading = "一个入口，连接多种 AI 模型 API"
			copy.Lead = "Coder Proxy 是 AI 聚合平台。你可以通过统一 API 调用本站支持的模型，在同一账户中查看当前价格、管理密钥并追踪用量。"
			copy.Sections = []siteSEOSection{{"接入 AI 模型", "通过本站支持的 API 协议和模型路由连接应用。具体可用模型和能力以当前服务配置为准。"}, {"了解费用", "使用前查看公开定价页；登录后可以在控制台查看用量与费用记录。"}, {"参考接入文档", "创建账户、获取 API 密钥，并根据所选协议的文档完成接入。"}}
			copy.ContactIntro = "关于接入、计费或使用问题，可以通过以下方式联系。"
		}
	case "about":
		copy.Title = "New API | About Coder Proxy"
		copy.SocialTitle = "About Coder Proxy"
		copy.Description = "Learn what Coder Proxy provides, how its AI API gateway works, and how to contact the service."
		copy.Heading = "About Coder Proxy"
		copy.Lead = "Coder Proxy provides a hosted AI aggregation platform for people building applications with AI models."
		copy.Sections = []siteSEOSection{{"What the service does", "The platform provides account access, API credentials, model routing, pricing information, and usage records in one place."}, {"What to check before use", "Supported providers, models, capabilities, and prices can change. Review the live pricing page and integration documentation before choosing a route."}, {"Project attribution", "Coder Proxy uses the New API gateway project. The original New API project and QuantumNous attribution remain with that project; this site's support channels are listed below."}}
		copy.ContactIntro = "For questions about this service, contact us directly."
		if lang == "zh-CN" {
			copy.Title = "New API | 关于 Coder Proxy"
			copy.SocialTitle = "关于 Coder Proxy"
			copy.Description = "了解 Coder Proxy 的 AI 聚合服务、统一 API 接入方式与本站联系方式。"
			copy.Heading = "关于 Coder Proxy"
			copy.Lead = "Coder Proxy 为需要调用 AI 模型的开发者和应用提供托管的 AI 聚合服务。"
			copy.Sections = []siteSEOSection{{"服务内容", "本站集中提供账户、API 密钥、模型路由、价格查询和用量记录，便于在一个平台接入与管理 AI 调用。"}, {"使用前确认", "支持的提供方、模型、能力与价格可能变化。选择路由前，请查看实时定价页与接入文档。"}, {"项目归属", "Coder Proxy 使用 New API 网关项目。New API 与 QuantumNous 的原项目署名保持不变；本站支持渠道见下方。"}}
			copy.ContactIntro = "如需了解本站服务，可直接联系。"
		}
	case "contact":
		copy.Title = "New API | Contact Coder Proxy"
		copy.SocialTitle = "Contact Coder Proxy"
		copy.Description = "Contact Coder Proxy about AI API access, billing, or integration through Telegram or email."
		copy.Heading = "Contact Coder Proxy"
		copy.Lead = "For questions about account access, AI API integration, or billing on Coder Proxy, use one of the channels below."
		copy.ContactIntro = "Please describe the issue and relevant request time. Do not send API keys or passwords."
		if lang == "zh-CN" {
			copy.Title = "New API | 联系 Coder Proxy"
			copy.SocialTitle = "联系 Coder Proxy"
			copy.Description = "通过 Telegram 或电子邮件联系 Coder Proxy，咨询 AI API 接入、计费和使用问题。"
			copy.Heading = "联系 Coder Proxy"
			copy.Lead = "有关 Coder Proxy 的账户、AI API 接入或计费问题，请通过以下渠道联系。"
			copy.ContactIntro = "请说明问题和相关请求时间，不要发送 API 密钥或密码。"
		}
	case "privacy-policy":
		copy.Title = "New API | 隐私政策"
		copy.SocialTitle = "Coder Proxy 隐私政策"
		copy.Description = "了解 Coder Proxy 已发布的隐私政策。"
		copy.Heading = "隐私政策"
		copy.OtherLang = ""
	case "user-agreement":
		copy.Title = "New API | 用户协议"
		copy.SocialTitle = "Coder Proxy 用户协议"
		copy.Description = "阅读 Coder Proxy 已发布的用户协议。"
		copy.Heading = "用户协议"
		copy.OtherLang = ""
	}
	return copy
}

func buildSiteSEOPages(indexPage []byte) siteSEOPages {
	var pages siteSEOPages
	for legalState := range 4 {
		pages.home[legalState] = make(map[string][]byte)
		pages.about[legalState] = make(map[string][]byte)
		pages.contact[legalState] = make(map[string][]byte)
		for _, lang := range []string{"en", "zh-CN"} {
			for _, kind := range []string{"home", "about", "contact"} {
				copy := siteCopy(kind, lang)
				copy.ShowAgreement = legalState&1 != 0
				copy.ShowPrivacy = legalState&2 != 0
				page, err := renderSiteSEOPage(indexPage, copy)
				if err != nil {
					common.SysError("could not render public SEO page: " + err.Error())
					continue
				}
				switch kind {
				case "home":
					pages.home[legalState][lang] = page
				case "about":
					pages.about[legalState][lang] = page
				case "contact":
					pages.contact[legalState][lang] = page
				}
			}
		}
	}
	return pages
}

func siteSEOSitemap(includeHome, includeAbout, includeAgreement, includePrivacy bool) []byte {
	type sitemapURL struct {
		Loc string `xml:"loc"`
	}
	type sitemapSet struct {
		XMLName xml.Name     `xml:"urlset"`
		Xmlns   string       `xml:"xmlns,attr"`
		URLs    []sitemapURL `xml:"url"`
	}
	urls := []sitemapURL{}
	paths := []string{}
	if includeHome {
		paths = append(paths, "/")
	}
	if includeAbout {
		paths = append(paths, "/about")
	}
	paths = append(paths, "/contact")
	for _, path := range paths {
		urls = append(urls, sitemapURL{siteSEOOrigin + path}, sitemapURL{siteSEOOrigin + path + "?lang=zh-CN"})
	}
	if includeAgreement {
		urls = append(urls, sitemapURL{siteSEOOrigin + "/user-agreement"})
	}
	if includePrivacy {
		urls = append(urls, sitemapURL{siteSEOOrigin + "/privacy-policy"})
	}
	result, err := xml.MarshalIndent(sitemapSet{Xmlns: "http://www.sitemaps.org/schemas/sitemap/0.9", URLs: urls}, "", "  ")
	if err != nil {
		return nil
	}
	return append([]byte(xml.Header), result...)
}

func siteSEORenderableLegal(content string) bool {
	content = strings.TrimSpace(content)
	if content == "" || siteSEOHTMLPattern.MatchString(content) {
		return false
	}
	link, err := url.Parse(content)
	if err == nil && link.Host != "" && (link.Scheme == "http" || link.Scheme == "https") {
		return false
	}
	return true
}

func renderSiteSEOLegalPage(kind, content string, legalState int) ([]byte, error) {
	copy := siteCopy(kind, "zh-CN")
	copy.ShowAgreement, copy.ShowPrivacy = legalState&1 != 0, legalState&2 != 0
	var rendered bytes.Buffer
	if err := goldmark.Convert([]byte(content), &rendered); err != nil {
		return nil, err
	}
	// Goldmark's default renderer excludes raw HTML and unsafe links.
	copy.LegalContent = template.HTML(rendered.String())
	return renderSiteSEOPage(nil, copy)
}

func renderSiteSEOPage(indexPage []byte, copy siteSEOCopy) ([]byte, error) {
	base := indexPage
	if copy.PageKind != "home" {
		base = []byte("<!doctype html><html><head><meta charset=\"utf-8\"><meta name=\"viewport\" content=\"width=device-width, initial-scale=1\"></head><body></body></html>")
	}
	doc, err := html.Parse(bytes.NewReader(base))
	if err != nil {
		return nil, err
	}
	var root, head, body *html.Node
	var visit func(*html.Node)
	visit = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch n.Data {
			case "html":
				root = n
			case "head":
				head = n
			case "body":
				body = n
			}
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			visit(child)
		}
	}
	visit(doc)
	if root == nil || head == nil || body == nil {
		return nil, html.ErrBufferExceeded
	}
	setSEOAttr(root, "lang", copy.Lang)
	if copy.PageKind != "home" {
		title := &html.Node{Type: html.ElementNode, Data: "title"}
		title.AppendChild(&html.Node{Type: html.TextNode, Data: copy.Title})
		head.AppendChild(title)
		appendSEOMeta(head, "name", "description", copy.Description)
	}
	appendSEOMeta(head, "property", "og:type", "website")
	appendSEOMeta(head, "property", "og:site_name", "Coder Proxy")
	appendSEOMeta(head, "property", "og:title", copy.SocialTitle)
	appendSEOMeta(head, "property", "og:description", copy.Description)
	appendSEOMeta(head, "property", "og:image", siteSEOOrigin+"/logo.png")
	appendSEOMeta(head, "property", "og:image:alt", "Coder Proxy")
	appendSEOMeta(head, "property", "og:locale", map[string]string{"en": "en_US", "zh-CN": "zh_CN"}[copy.Lang])
	appendSEOMeta(head, "name", "twitter:card", "summary")
	appendSEOMeta(head, "name", "twitter:title", copy.SocialTitle)
	appendSEOMeta(head, "name", "twitter:description", copy.Description)
	appendSEOMeta(head, "name", "twitter:image", siteSEOOrigin+"/logo.png")
	canonical := siteSEOOrigin + copy.PagePath
	if copy.Lang == "zh-CN" && copy.OtherLang != "" {
		canonical += "?lang=zh-CN"
	}
	appendSEOMeta(head, "property", "og:url", canonical)
	head.AppendChild(&html.Node{Type: html.ElementNode, Data: "link", Attr: []html.Attribute{{Key: "rel", Val: "canonical"}, {Key: "href", Val: canonical}}})
	if copy.OtherLang != "" {
		for _, alternate := range []struct{ lang, url string }{{"en", siteSEOOrigin + copy.PagePath}, {"zh-CN", siteSEOOrigin + copy.PagePath + "?lang=zh-CN"}, {"x-default", siteSEOOrigin + copy.PagePath}} {
			head.AppendChild(&html.Node{Type: html.ElementNode, Data: "link", Attr: []html.Attribute{{Key: "rel", Val: "alternate"}, {Key: "hreflang", Val: alternate.lang}, {Key: "href", Val: alternate.url}}})
		}
	}
	website, err := common.Marshal(map[string]any{"@context": "https://schema.org", "@type": "WebSite", "name": "Coder Proxy", "url": siteSEOOrigin + "/", "inLanguage": []string{"en", "zh-CN"}})
	if err != nil {
		return nil, err
	}
	script := &html.Node{Type: html.ElementNode, Data: "script", Attr: []html.Attribute{{Key: "type", Val: "application/ld+json"}}}
	script.AppendChild(&html.Node{Type: html.TextNode, Data: string(website)})
	head.AppendChild(script)
	style := &html.Node{Type: html.ElementNode, Data: "style"}
	style.AppendChild(&html.Node{Type: html.TextNode, Data: siteSEOStyle})
	head.AppendChild(style)
	var content bytes.Buffer
	if err := siteSEOBody.Execute(&content, copy); err != nil {
		return nil, err
	}
	fragments, err := html.ParseFragment(&content, &html.Node{Type: html.ElementNode, Data: "body", DataAtom: atom.Body})
	if err != nil {
		return nil, err
	}
	for _, fragment := range fragments {
		body.AppendChild(fragment)
	}
	if copy.PageKind == "home" {
		init := &html.Node{Type: html.ElementNode, Data: "script"}
		init.AppendChild(&html.Node{Type: html.TextNode, Data: `(function(){var lang=new URLSearchParams(location.search).get('lang');try{var saved=localStorage.getItem('i18nextLng');if(lang==='zh-CN'){localStorage.setItem('i18nextLng','zhCN')}else if(saved==='zhCN'||!saved&&/^(zh-CN|zh)$/i.test(navigator.language)){location.replace('/?lang=zh-CN');return}}catch(e){}var root=document.getElementById('root');var fallback=document.getElementById('seo-initial');if(!root||!fallback)return;var observer=new MutationObserver(function(){if(root.firstChild){fallback.remove();observer.disconnect()}});observer.observe(root,{childList:true});})();`})
		body.AppendChild(init)
	}
	var output bytes.Buffer
	if err := html.Render(&output, doc); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

func appendSEOMeta(head *html.Node, key, name, value string) {
	head.AppendChild(&html.Node{Type: html.ElementNode, Data: "meta", Attr: []html.Attribute{{Key: key, Val: name}, {Key: "content", Val: value}}})
}

func setSEOAttr(node *html.Node, key, value string) {
	for i := range node.Attr {
		if node.Attr[i].Key == key {
			node.Attr[i].Val = value
			return
		}
	}
	node.Attr = append(node.Attr, html.Attribute{Key: key, Val: value})
}

func (pages siteSEOPages) serve(c *gin.Context) {
	if strings.TrimRight(system_setting.ServerAddress, "/") != siteSEOOrigin {
		c.Next()
		return
	}
	if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead {
		c.Next()
		return
	}
	lang := "en"
	if c.Query("lang") == "zh-CN" {
		lang = "zh-CN"
	}
	common.OptionMapRWMutex.RLock()
	hasCustomHome := common.OptionMap["HomePageContent"] != ""
	hasCustomAbout := common.OptionMap["About"] != ""
	common.OptionMapRWMutex.RUnlock()
	legal := system_setting.GetLegalSettings()
	legalState := 0
	if legal.UserAgreement != "" {
		legalState |= 1
	}
	if legal.PrivacyPolicy != "" {
		legalState |= 2
	}
	var content []byte
	contentType := "text/html; charset=utf-8"
	switch c.Request.URL.Path {
	case "/":
		if hasCustomHome {
			c.Next()
			return
		}
		content = pages.home[legalState][lang]
	case "/about", "/about/":
		if hasCustomAbout {
			c.Next()
			return
		}
		content = pages.about[legalState][lang]
	case "/contact", "/contact/":
		content = pages.contact[legalState][lang]
	case "/robots.txt":
		contentType = "text/plain; charset=utf-8"
		content = []byte("User-agent: *\nAllow: /\nDisallow: /dashboard\nDisallow: /system-settings\n\nUser-agent: GPTBot\nDisallow: /\n\nUser-agent: ClaudeBot\nDisallow: /\n\nUser-agent: Google-Extended\nDisallow: /\n\nUser-agent: Applebot-Extended\nDisallow: /\n\nUser-agent: meta-externalagent\nDisallow: /\n\nUser-agent: Amazonbot\nDisallow: /\n\nUser-agent: CCBot\nDisallow: /\n\nUser-agent: Bytespider\nDisallow: /\n\nSitemap: " + siteSEOOrigin + "/sitemap.xml\n")
	case "/sitemap.xml":
		contentType = "application/xml; charset=utf-8"
		content = siteSEOSitemap(!hasCustomHome, !hasCustomAbout, siteSEORenderableLegal(legal.UserAgreement), siteSEORenderableLegal(legal.PrivacyPolicy))
	case "/llms.txt":
		contentType = "text/plain; charset=utf-8"
		var links strings.Builder
		links.WriteString("# Coder Proxy\n\nAI API aggregation platform / AI 模型聚合 API 平台.\n\n## 页面\n")
		if !hasCustomHome {
			links.WriteString("- [首页（英文）](" + siteSEOOrigin + "/)\n- [首页（简体中文）](" + siteSEOOrigin + "/?lang=zh-CN)\n")
		}
		if !hasCustomAbout {
			links.WriteString("- [关于本站](" + siteSEOOrigin + "/about?lang=zh-CN)\n")
		}
		links.WriteString("- [联系我们](" + siteSEOOrigin + "/contact?lang=zh-CN)\n")
		if siteSEORenderableLegal(legal.UserAgreement) {
			links.WriteString("- [用户协议](" + siteSEOOrigin + "/user-agreement)\n")
		}
		if siteSEORenderableLegal(legal.PrivacyPolicy) {
			links.WriteString("- [隐私政策](" + siteSEOOrigin + "/privacy-policy)\n")
		}
		content = []byte(links.String())
	case "/user-agreement":
		if legalState&1 == 0 {
			c.Header("Cache-Control", "no-store")
			c.Status(http.StatusNotFound)
			c.Abort()
			return
		}
		if !siteSEORenderableLegal(legal.UserAgreement) {
			c.Next()
			return
		}
		var err error
		content, err = renderSiteSEOLegalPage("user-agreement", legal.UserAgreement, legalState)
		if err != nil {
			c.Header("Cache-Control", "no-store")
			c.Status(http.StatusInternalServerError)
			c.Abort()
			return
		}
	case "/privacy-policy":
		if legalState&2 == 0 {
			c.Header("Cache-Control", "no-store")
			c.Status(http.StatusNotFound)
			c.Abort()
			return
		}
		if !siteSEORenderableLegal(legal.PrivacyPolicy) {
			c.Next()
			return
		}
		var err error
		content, err = renderSiteSEOLegalPage("privacy-policy", legal.PrivacyPolicy, legalState)
		if err != nil {
			c.Header("Cache-Control", "no-store")
			c.Status(http.StatusInternalServerError)
			c.Abort()
			return
		}
	default:
		if strings.HasSuffix(c.Request.URL.Path, ".xml") || strings.HasSuffix(c.Request.URL.Path, ".txt") {
			c.Header("Cache-Control", "no-store")
			c.Status(http.StatusNotFound)
			c.Abort()
			return
		}
		c.Next()
		return
	}
	if len(content) == 0 {
		c.Next()
		return
	}
	if contentType == "text/html; charset=utf-8" {
		c.Header("Cache-Control", "no-cache, no-transform")
	} else {
		c.Header("Cache-Control", "no-cache")
	}
	c.Data(http.StatusOK, contentType, content)
	c.Abort()
}

func siteSEORejectsSPAFallback(path string) bool {
	if strings.TrimRight(system_setting.ServerAddress, "/") != siteSEOOrigin {
		return false
	}
	path = strings.TrimSuffix(path, "/")
	switch path {
	case "", "/", "/about", "/contact", "/pricing", "/group-monitor", "/rankings", "/setup", "/oauth", "/sign-in", "/sign-up", "/register", "/forgot-password", "/reset", "/otp", "/user/reset", "/privacy-policy", "/user-agreement", "/401", "/403", "/404", "/500", "/503", "/chat2link", "/channel-monitor", "/channels", "/dashboard", "/keys", "/models", "/playground", "/profile", "/redemption-codes", "/security", "/subscriptions", "/system-info", "/system-settings", "/task-plugins", "/usage-logs", "/users", "/wallet", "/wallet/shop", "/console", "/login", "/forbidden":
		return false
	}
	part, rest, found := strings.Cut(strings.TrimPrefix(path, "/"), "/")
	if !found || rest == "" {
		return true
	}
	if part == "console" || part == "system-settings" {
		return false
	}
	if strings.Contains(rest, "/") {
		return true
	}
	switch part {
	case "pricing", "oauth", "chat", "dashboard", "models", "errors", "usage-logs":
		return false
	default:
		return true
	}
}

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
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/yuin/goldmark"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

const siteSEOOrigin = "https://coder.work-proxy.com"
const siteSEOContentModified = "2026-11-06"

var siteSEOHTMLPattern = regexp.MustCompile(`(?i)<!doctype html|<html[\s>]|<head[\s>]|<body[\s>]|<style[\s>]|<script[\s>]|</?[a-z][\s\S]*>`)

type siteSEOPages struct {
	home    [4]map[string][]byte
	pricing [4]map[string][]byte
	about   [4]map[string][]byte
	contact [4]map[string][]byte
}

type siteSEOCopy struct {
	Lang            string
	OtherLang       string
	OtherLabel      string
	HomeLabel       string
	AboutLabel      string
	ContactLabel    string
	PricingLabel    string
	DocsLabel       string
	SignupLabel     string
	Title           string
	SocialTitle     string
	Description     string
	Heading         string
	Lead            string
	Sections        []siteSEOSection
	ContactIntro    string
	ProjectNote     string
	MaintainerLabel string
	UpdatedLabel    string
	UpdatedDate     string
	ModifiedDate    string
	PagePath        string
	PageKind        string
	ShowAgreement   bool
	ShowPrivacy     bool
	LegalContent    template.HTML
}

type siteSEOSection struct {
	Title          string
	Text           string
	ReferenceLabel string
	ReferenceURL   string
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
  #seo-initial .seo-sections .seo-reference { margin-top: 10px; font-size: 14px; }
  #seo-initial .seo-sections .seo-reference a { color: #17666e; }
  #seo-initial .seo-byline { color: #596771; font-size: 14px; margin: 0 0 28px; }
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
        <a href="/pricing{{if eq .Lang "zh-CN"}}?lang=zh-CN{{end}}">{{.PricingLabel}}</a>
        <a href="/contact{{if eq .Lang "zh-CN"}}?lang=zh-CN{{end}}">{{.ContactLabel}}</a>
        {{if .OtherLang}}<a class="seo-language" href="{{.PagePath}}{{if eq .OtherLang "zh-CN"}}?lang=zh-CN{{end}}" lang="{{.OtherLang}}">{{.OtherLabel}}</a>{{end}}
      </nav>
    </header>
    <main>
      <p class="seo-eyebrow">Coder Proxy</p>
      <h1>{{.Heading}}</h1>
      {{if .Lead}}<p class="seo-lead">{{.Lead}}</p>{{end}}
      {{if .ModifiedDate}}<p class="seo-byline">{{.MaintainerLabel}}: Coder Proxy · <time datetime="{{.ModifiedDate}}">{{.UpdatedLabel}} {{.UpdatedDate}}</time></p>{{end}}
      {{if eq .PageKind "home"}}
      <div class="seo-actions">
        <a href="/sign-up">{{.SignupLabel}}</a>
        <a href="/pricing{{if eq .Lang "zh-CN"}}?lang=zh-CN{{end}}">{{.PricingLabel}}</a>
        <a href="https://docs.newapi.pro/">{{.DocsLabel}}</a>
      </div>
      {{end}}
      {{if .Sections}}
      <div class="seo-sections">
        {{range .Sections}}<section><h2>{{.Title}}</h2><p>{{.Text}}</p>{{if .ReferenceURL}}<p class="seo-reference"><a href="{{.ReferenceURL}}">{{.ReferenceLabel}}</a></p>{{end}}</section>{{end}}
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
	copy := siteSEOCopy{Lang: lang, PageKind: kind, PagePath: "/", OtherLang: "zh-CN", OtherLabel: "简体中文", HomeLabel: "Home", AboutLabel: "About", ContactLabel: "Contact", PricingLabel: "Pricing", DocsLabel: "Documentation", SignupLabel: "Get started", ProjectNote: "The gateway software is based on the New API project by", MaintainerLabel: "Maintained by", UpdatedLabel: "Updated", UpdatedDate: "November 6, 2026"}
	if kind != "home" {
		copy.PagePath = "/" + kind
	}
	if lang == "zh-CN" {
		copy.OtherLang, copy.OtherLabel = "en", "English"
		copy.HomeLabel, copy.AboutLabel, copy.ContactLabel = "首页", "关于我们", "联系我们"
		copy.PricingLabel, copy.DocsLabel, copy.SignupLabel = "查看价格", "使用文档", "开始使用"
		copy.ProjectNote = "本站网关软件基于 New API 项目，原项目及作者："
		copy.MaintainerLabel, copy.UpdatedLabel, copy.UpdatedDate = "内容维护", "更新于", "2026 年 11 月 06 日"
	}
	switch kind {
	case "home":
		copy.ModifiedDate = siteSEOContentModified
		copy.Title = "New API"
		copy.SocialTitle = "Coder Proxy | AI API aggregation platform"
		copy.Description = "Access multiple AI models through Coder Proxy's unified API gateway. Explore pricing, create an account, and connect your applications."
		copy.Heading = "One gateway for AI model APIs"
		copy.Lead = "Coder Proxy is an AI aggregation platform. Use one account to access supported models through a unified API, review current pricing, and manage your usage from one dashboard."
		copy.Sections = []siteSEOSection{{"Access AI models", "Connect applications through supported API protocols and model routes. Available models and capabilities depend on the current service configuration.", "Current model pricing", "/pricing"}, {"Understand costs", "Check the public pricing page before use. Usage and charges can be reviewed in your account dashboard.", "Current pricing", "/pricing"}, {"Start with documentation", "Create an account, obtain an API key, and follow the integration documentation for your chosen protocol.", "Gateway software documentation", "https://docs.newapi.pro/"}}
		copy.ContactIntro = "Questions about access, billing, or integration? Reach us through these channels."
		if lang == "zh-CN" {
			copy.SocialTitle = "Coder Proxy｜AI 模型聚合 API 平台"
			copy.Description = "Coder Proxy（亦称 Coder API）提供统一 AI 模型 API 接入。查看当前价格、创建账户，并在控制台管理用量。"
			copy.Heading = "一个入口，连接多种 AI 模型 API"
			copy.Lead = "Coder Proxy（亦称 Coder API）是 AI 聚合平台。你可以通过统一 API 调用本站支持的模型，在同一账户中查看当前价格、管理密钥并追踪用量。"
			copy.Sections = []siteSEOSection{
				{"Coder Proxy 是什么？", "Coder Proxy 是面向开发者的托管 AI API 聚合服务。你通过本站账户和统一接口调用当前开放的模型，不需要自行部署网关或管理上游渠道。本站与网关软件的功能范围不同，实际可用模型和接口以公开定价页及接入文档为准。", "关于本站", "/about?lang=zh-CN"},
				{"如何开始调用？", "先创建账户，在控制台获取本站 API 密钥，再按所选协议的接入文档配置请求地址和模型名称。密钥用于你的应用调用，请勿放在公开网页或提交到代码仓库。完成接入后，可在账户中查看调用记录。", "网关软件接入文档", "https://docs.newapi.pro/"},
				{"支持哪些模型和接口？", "可用范围取决于本站当前配置，不等同于网关软件支持的全部提供方、模型或端点。选择模型前，先在定价页查看当前展示的模型与计费方式；再结合接入文档确认所需协议和参数。页面未列出的能力不要视为已经开放。", "本站当前定价", "/pricing?lang=zh-CN"},
				{"费用与用量在哪里查看？", "使用前可公开查看定价页。实际价格、单位及适用条件以该页当前数据为准，不应依赖旧截图或固定宣传数字。登录后可在控制台查看账户用量与费用记录；如对记录有疑问，可提供请求时间和相关标识联系本站支持。", "本站当前定价", "/pricing?lang=zh-CN"},
			}
			copy.ContactIntro = "关于接入、计费或使用问题，可以通过以下方式联系。"
		}
	case "pricing":
		copy.ModifiedDate = siteSEOContentModified
		home := siteCopy("home", lang)
		copy.Title = "New API | " + copy.PricingLabel
		copy.SocialTitle, copy.Description = "Coder Proxy | "+copy.PricingLabel, home.Sections[1].Text
		copy.Heading, copy.Lead = copy.PricingLabel, home.Sections[1].Text
		about := siteCopy("about", lang)
		copy.Sections, copy.ContactIntro = []siteSEOSection{home.Sections[1], about.Sections[1], home.Sections[2]}, home.ContactIntro
		if lang == "zh-CN" {
			copy.SocialTitle = "Coder Proxy｜当前模型价格与用量说明"
			copy.Description = "查看 Coder Proxy 当前开放模型的价格、计费单位与用量记录入口。具体价格以实时定价数据为准。"
			copy.Heading = "如何查看 Coder Proxy 当前价格？"
			copy.Lead = "本站的模型价格随服务配置更新。请在下方交互式定价列表查看当前开放的模型、价格和适用条件；登录后可在控制台核对自己的调用用量与费用记录。"
			copy.Sections = []siteSEOSection{
				{"价格以什么为准？", "以本页实时展示的数据为准。不同模型和计费方式可能使用不同单位，比较前请查看对应模型的详细说明；不要把过去保存的截图、示例金额或其他站点的价格视为本站现价。", "联系价格支持", "/contact?lang=zh-CN"},
				{"如何选择模型？", "先在定价列表确认本站当前开放的模型，再核对所需接口、能力和适用条件。列表会随服务配置变化，不能把网关软件支持的全部模型视为本站已经开放。", "网关软件接入文档", "https://docs.newapi.pro/"},
				{"如何核对用量？", "登录后在控制台查看调用记录、用量与费用。如对某次调用有疑问，请通过联系页面提供相关请求时间和标识，不要发送 API 密钥或密码。", "联系本站支持", "/contact?lang=zh-CN"},
			}
			copy.ContactIntro = "对价格、用量或接入有疑问，可通过以下渠道联系本站。"
		}
	case "about":
		copy.Title = "New API | About Coder Proxy"
		copy.SocialTitle = "About Coder Proxy"
		copy.Description = "Learn what Coder Proxy provides, how its AI API gateway works, and how to contact the service."
		copy.Heading = "About Coder Proxy"
		copy.Lead = "Coder Proxy provides a hosted AI aggregation platform for people building applications with AI models."
		copy.Sections = []siteSEOSection{{Title: "What the service does", Text: "The platform provides account access, API credentials, model routing, pricing information, and usage records in one place."}, {Title: "What to check before use", Text: "Supported providers, models, capabilities, and prices can change. Review the live pricing page and integration documentation before choosing a route."}, {Title: "Project attribution", Text: "Coder Proxy uses the New API gateway project. The original New API project and QuantumNous attribution remain with that project; this site's support channels are listed below."}}
		copy.ContactIntro = "For questions about this service, contact us directly."
		if lang == "zh-CN" {
			copy.Title = "New API | 关于 Coder Proxy"
			copy.SocialTitle = "关于 Coder Proxy"
			copy.Description = "了解 Coder Proxy（亦称 Coder API）的 AI 聚合服务、统一 API 接入方式与本站联系方式。"
			copy.Heading = "关于 Coder Proxy"
			copy.Lead = "Coder Proxy 与 Coder API 是本站同一服务的两个名称，为需要调用 AI 模型的开发者和应用提供托管的 AI 聚合服务。"
			copy.Sections = []siteSEOSection{{Title: "服务内容", Text: "本站集中提供账户、API 密钥、模型路由、价格查询和用量记录，便于在一个平台接入与管理 AI 调用。"}, {Title: "使用前确认", Text: "支持的提供方、模型、能力与价格可能变化。选择路由前，请查看实时定价页与接入文档。"}, {Title: "项目归属", Text: "Coder Proxy 使用 New API 网关项目。New API 与 QuantumNous 的原项目署名保持不变；本站支持渠道见下方。"}}
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
		copy.Description = "了解 Coder API（亦称 Coder Proxy）已发布的隐私政策。"
		copy.Heading = "隐私政策"
		copy.OtherLang = ""
	case "user-agreement":
		copy.Title = "New API | 用户协议"
		copy.SocialTitle = "Coder Proxy 用户协议"
		copy.Description = "阅读 Coder API（亦称 Coder Proxy）已发布的用户协议。"
		copy.Heading = "用户协议"
		copy.OtherLang = ""
	}
	return copy
}

func buildSiteSEOPages(indexPage []byte) siteSEOPages {
	var pages siteSEOPages
	for legalState := range 4 {
		pages.home[legalState] = make(map[string][]byte)
		pages.pricing[legalState] = make(map[string][]byte)
		pages.about[legalState] = make(map[string][]byte)
		pages.contact[legalState] = make(map[string][]byte)
		for _, lang := range []string{"en", "zh-CN"} {
			for _, kind := range []string{"home", "pricing", "about", "contact"} {
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
				case "pricing":
					pages.pricing[legalState][lang] = page
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

func siteSEOSitemap(includeHome, includeAbout, includePricing, includeAgreement, includePrivacy bool) []byte {
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
	if includePricing {
		paths = append(paths, "/pricing")
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
	if copy.PageKind != "home" && copy.PageKind != "pricing" {
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
	if copy.PageKind == "pricing" {
		for child := head.FirstChild; child != nil; child = child.NextSibling {
			if child.Type == html.ElementNode && child.Data == "title" && child.FirstChild != nil {
				child.FirstChild.Data = copy.Title
			}
			if child.Type == html.ElementNode && child.Data == "meta" {
				for i := range child.Attr {
					if child.Attr[i].Key == "name" {
						switch child.Attr[i].Val {
						case "description":
							setSEOAttr(child, "content", copy.Description)
						case "title":
							setSEOAttr(child, "content", copy.Title)
						}
					}
				}
			}
		}
	} else if copy.PageKind != "home" {
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
	website, err := common.Marshal(map[string]any{"@context": "https://schema.org", "@type": "WebSite", "name": "Coder Proxy", "alternateName": "Coder API", "url": siteSEOOrigin + "/", "inLanguage": []string{"en", "zh-CN"}})
	if err != nil {
		return nil, err
	}
	script := &html.Node{Type: html.ElementNode, Data: "script", Attr: []html.Attribute{{Key: "type", Val: "application/ld+json"}}}
	script.AppendChild(&html.Node{Type: html.TextNode, Data: string(website)})
	head.AppendChild(script)
	if copy.ModifiedDate != "" {
		appendSEOMeta(head, "name", "author", "Coder Proxy")
		organizationID := siteSEOOrigin + "/#organization"
		serviceID := siteSEOOrigin + "/#service"
		organization := map[string]any{
			"@type": "Organization", "@id": organizationID, "name": "Coder Proxy", "alternateName": "Coder API", "url": siteSEOOrigin + "/",
			"contactPoint": map[string]any{"@type": "ContactPoint", "contactType": "customer support", "email": "krismiledev@gmail.com", "url": "https://t.me/krismileji"},
		}
		webpage := map[string]any{
			"@type": "WebPage", "@id": canonical + "#webpage", "url": canonical, "name": copy.Heading,
			"description": copy.Description, "inLanguage": copy.Lang, "dateModified": copy.ModifiedDate,
			"author": map[string]any{"@id": organizationID}, "publisher": map[string]any{"@id": organizationID},
			"about": map[string]any{"@id": serviceID},
		}
		graph := []map[string]any{organization, webpage}
		if copy.PageKind == "home" {
			graph = append(graph, map[string]any{
				"@type": "Service", "@id": serviceID, "name": "Coder Proxy", "alternateName": "Coder API", "serviceType": "AI API gateway",
				"url": siteSEOOrigin + "/", "description": copy.Lead,
				"provider": map[string]any{"@id": organizationID},
			})
		}
		structured, err := common.Marshal(map[string]any{"@context": "https://schema.org", "@graph": graph})
		if err != nil {
			return nil, err
		}
		entityScript := &html.Node{Type: html.ElementNode, Data: "script", Attr: []html.Attribute{{Key: "type", Val: "application/ld+json"}}}
		entityScript.AppendChild(&html.Node{Type: html.TextNode, Data: string(structured)})
		head.AppendChild(entityScript)
	}
	style := &html.Node{Type: html.ElementNode, Data: "style"}
	style.AppendChild(&html.Node{Type: html.TextNode, Data: siteSEOStyle})
	head.AppendChild(style)
	if copy.PageKind == "home" || copy.PageKind == "pricing" {
		publicJSON, err := siteSEOPublicJSON(copy)
		if err != nil {
			return nil, err
		}
		publicScript := &html.Node{Type: html.ElementNode, Data: "script", Attr: []html.Attribute{{Key: "id", Val: "site-public-content"}, {Key: "type", Val: "application/json"}}}
		publicScript.AppendChild(&html.Node{Type: html.TextNode, Data: string(publicJSON)})
		head.AppendChild(publicScript)
	}
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
	if copy.PageKind == "home" || copy.PageKind == "pricing" {
		init := &html.Node{Type: html.ElementNode, Data: "script"}
		init.AppendChild(&html.Node{Type: html.TextNode, Data: `(function(){var lang=new URLSearchParams(location.search).get('lang');try{var saved=localStorage.getItem('i18nextLng');if(lang==='zh-CN'){localStorage.setItem('i18nextLng','zhCN')}else if(saved==='zhCN'||!saved&&/^(zh-CN|zh)$/i.test(navigator.language)){var target=new URL(location.href);target.searchParams.set('lang','zh-CN');location.replace(target.pathname+target.search);return}}catch(e){}var root=document.getElementById('root');var fallback=document.getElementById('seo-initial');if(!root||!fallback)return;root.style.display='none';var observer=new MutationObserver(function(){if(root.querySelector('[data-site-public-ready]')){root.style.display='';fallback.remove();observer.disconnect()}});observer.observe(root,{childList:true,subtree:true});})();`})
		body.AppendChild(init)
	}
	var output bytes.Buffer
	if err := html.Render(&output, doc); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

func siteSEOPublicJSON(copy siteSEOCopy) ([]byte, error) {
	return common.Marshal(map[string]any{
		"kind":            copy.PageKind,
		"lang":            copy.Lang,
		"title":           copy.Title,
		"heading":         copy.Heading,
		"lead":            copy.Lead,
		"sections":        copy.Sections,
		"contactIntro":    copy.ContactIntro,
		"contactLabel":    copy.ContactLabel,
		"aboutLabel":      copy.AboutLabel,
		"projectNote":     copy.ProjectNote,
		"maintainerLabel": copy.MaintainerLabel,
		"updatedLabel":    copy.UpdatedLabel,
		"updatedDate":     copy.UpdatedDate,
		"modifiedDate":    copy.ModifiedDate,
		"signupLabel":     copy.SignupLabel,
		"pricingLabel":    copy.PricingLabel,
		"docsLabel":       copy.DocsLabel,
	})
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
	pricingPublic := middleware.HeaderNavModuleIsPublic("pricing")
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
	case "/pricing", "/pricing/":
		if !pricingPublic {
			c.Header("X-Robots-Tag", "noindex")
			c.Next()
			return
		}
		content = pages.pricing[legalState][lang]
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
		content = siteSEOSitemap(!hasCustomHome, !hasCustomAbout, pricingPublic, siteSEORenderableLegal(legal.UserAgreement), siteSEORenderableLegal(legal.PrivacyPolicy))
	case "/site-public-content.json":
		kind := c.Query("page")
		if kind != "home" && kind != "pricing" {
			c.Status(http.StatusNotFound)
			c.Abort()
			return
		}
		if kind == "pricing" && !pricingPublic {
			c.Status(http.StatusNotFound)
			c.Abort()
			return
		}
		contentType = "application/json; charset=utf-8"
		c.Header("X-Robots-Tag", "noindex")
		var err error
		content, err = siteSEOPublicJSON(siteCopy(kind, lang))
		if err != nil {
			c.Status(http.StatusInternalServerError)
			c.Abort()
			return
		}
	case "/llms.txt":
		contentType = "text/plain; charset=utf-8"
		var links strings.Builder
		links.WriteString("# Coder Proxy\n\nCoder API 是 Coder Proxy 的别名，同属本站 AI 模型聚合 API 服务。\n\n## 页面\n")
		if !hasCustomHome {
			links.WriteString("- [首页（英文）](" + siteSEOOrigin + "/)\n- [首页（简体中文）](" + siteSEOOrigin + "/?lang=zh-CN)\n")
		}
		if !hasCustomAbout {
			links.WriteString("- [关于本站](" + siteSEOOrigin + "/about?lang=zh-CN)\n")
		}
		if pricingPublic {
			links.WriteString("- [当前价格](" + siteSEOOrigin + "/pricing?lang=zh-CN)\n")
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

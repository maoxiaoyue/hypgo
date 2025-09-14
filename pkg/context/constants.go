package context

import "math"

// ===== 通用常量 =====

const (
	// defaultMemory 默認的表單內存限制 (32 MB)
	defaultMemory = 32 << 20

	// abortIndex 中止索引
	abortIndex = math.MaxInt8 / 2
)

// ===== MIME 類型常量 =====

const (
	// MIMEJSON JSON 內容類型
	MIMEJSON = "application/json"
	// MIMEHTML HTML 內容類型
	MIMEHTML = "text/html"
	// MIMEXML XML 內容類型
	MIMEXML = "application/xml"
	// MIMEXML2 XML 內容類型（備選）
	MIMEXML2 = "text/xml"
	// MIMEPlain 純文本內容類型
	MIMEPlain = "text/plain"
	// MIMEPOSTForm 表單 POST 內容類型
	MIMEPOSTForm = "application/x-www-form-urlencoded"
	// MIMEMultipartPOSTForm 多部分表單內容類型
	MIMEMultipartPOSTForm = "multipart/form-data"
	// MIMEPROTOBUF Protocol Buffer 內容類型
	MIMEPROTOBUF = "application/x-protobuf"
	// MIMEMSGPACK MessagePack 內容類型
	MIMEMSGPACK = "application/x-msgpack"
	// MIMEMSGPACK2 MessagePack 內容類型（備選）
	MIMEMSGPACK2 = "application/msgpack"
	// MIMEYAML YAML 內容類型
	MIMEYAML = "application/x-yaml"
	// MIMEYAML2 YAML 內容類型（備選）
	MIMEYAML2 = "application/yaml"
	// MIMETOML TOML 內容類型
	MIMETOML = "application/toml"
)

// ===== 錯誤類型常量 =====

const (
	// ErrorTypeBind 綁定錯誤
	ErrorTypeBind ErrorType = 1 << 63
	// ErrorTypeRender 渲染錯誤
	ErrorTypeRender ErrorType = 1 << 62
	// ErrorTypePrivate 私有錯誤
	ErrorTypePrivate ErrorType = 1 << 61
	// ErrorTypePublic 公開錯誤
	ErrorTypePublic ErrorType = 1 << 60
	// ErrorTypeAny 任意錯誤
	ErrorTypeAny ErrorType = 1<<64 - 1
	// ErrorTypeNu 無錯誤
	ErrorTypeNu = 2
)

// ===== HTTP 狀態碼常量（擴展）=====

const (
	// StatusTooEarly 425 Too Early (RFC 8470)
	StatusTooEarly = 425
	// StatusUnavailableForLegalReasons 451 Unavailable For Legal Reasons (RFC 7725)
	StatusUnavailableForLegalReasons = 451
)

// ===== 協議類型常量 =====

type Protocol int

const (
	// HTTP1 HTTP/1.x 協議
	HTTP1 Protocol = iota
	// HTTP2 HTTP/2 協議
	HTTP2
	// HTTP3 HTTP/3 協議
	HTTP3
)

// String 返回協議字符串表示
func (p Protocol) String() string {
	switch p {
	case HTTP3:
		return "HTTP/3"
	case HTTP2:
		return "HTTP/2"
	default:
		return "HTTP/1.1"
	}
}

// ===== 默認值常量 =====

const (
	// DefaultMaxMemory 默認最大內存
	DefaultMaxMemory = 32 << 20 // 32 MB

	// DefaultPageSize 默認分頁大小
	DefaultPageSize = 10

	// MaxPageSize 最大分頁大小
	MaxPageSize = 100

	// DefaultTimeout 默認超時時間（秒）
	DefaultTimeout = 30
)

// ===== Header 名稱常量 =====

const (
	// HeaderAccept Accept header
	HeaderAccept = "Accept"
	// HeaderAcceptEncoding Accept-Encoding header
	HeaderAcceptEncoding = "Accept-Encoding"
	// HeaderAllow Allow header
	HeaderAllow = "Allow"
	// HeaderAuthorization Authorization header
	HeaderAuthorization = "Authorization"
	// HeaderContentDisposition Content-Disposition header
	HeaderContentDisposition = "Content-Disposition"
	// HeaderContentEncoding Content-Encoding header
	HeaderContentEncoding = "Content-Encoding"
	// HeaderContentLength Content-Length header
	HeaderContentLength = "Content-Length"
	// HeaderContentType Content-Type header
	HeaderContentType = "Content-Type"
	// HeaderCookie Cookie header
	HeaderCookie = "Cookie"
	// HeaderSetCookie Set-Cookie header
	HeaderSetCookie = "Set-Cookie"
	// HeaderIfModifiedSince If-Modified-Since header
	HeaderIfModifiedSince = "If-Modified-Since"
	// HeaderLastModified Last-Modified header
	HeaderLastModified = "Last-Modified"
	// HeaderLocation Location header
	HeaderLocation = "Location"
	// HeaderUpgrade Upgrade header
	HeaderUpgrade = "Upgrade"
	// HeaderVary Vary header
	HeaderVary = "Vary"
	// HeaderWWWAuthenticate WWW-Authenticate header
	HeaderWWWAuthenticate = "WWW-Authenticate"
	// HeaderXForwardedFor X-Forwarded-For header
	HeaderXForwardedFor = "X-Forwarded-For"
	// HeaderXForwardedProto X-Forwarded-Proto header
	HeaderXForwardedProto = "X-Forwarded-Proto"
	// HeaderXForwardedProtocol X-Forwarded-Protocol header
	HeaderXForwardedProtocol = "X-Forwarded-Protocol"
	// HeaderXForwardedSsl X-Forwarded-Ssl header
	HeaderXForwardedSsl = "X-Forwarded-Ssl"
	// HeaderXUrlScheme X-Url-Scheme header
	HeaderXUrlScheme = "X-Url-Scheme"
	// HeaderXHTTPMethodOverride X-HTTP-Method-Override header
	HeaderXHTTPMethodOverride = "X-HTTP-Method-Override"
	// HeaderXRealIP X-Real-IP header
	HeaderXRealIP = "X-Real-IP"
	// HeaderXRequestID X-Request-ID header
	HeaderXRequestID = "X-Request-ID"
	// HeaderXRequestedWith X-Requested-With header
	HeaderXRequestedWith = "X-Requested-With"
	// HeaderServer Server header
	HeaderServer = "Server"
	// HeaderOrigin Origin header
	HeaderOrigin = "Origin"
	// HeaderCacheControl Cache-Control header
	HeaderCacheControl = "Cache-Control"
	// HeaderPragma Pragma header
	HeaderPragma = "Pragma"
	// HeaderExpires Expires header
	HeaderExpires = "Expires"
	// HeaderETag ETag header
	HeaderETag = "ETag"
	// HeaderIfNoneMatch If-None-Match header
	HeaderIfNoneMatch = "If-None-Match"

	// CORS Headers
	HeaderAccessControlRequestMethod    = "Access-Control-Request-Method"
	HeaderAccessControlRequestHeaders   = "Access-Control-Request-Headers"
	HeaderAccessControlAllowOrigin      = "Access-Control-Allow-Origin"
	HeaderAccessControlAllowMethods     = "Access-Control-Allow-Methods"
	HeaderAccessControlAllowHeaders     = "Access-Control-Allow-Headers"
	HeaderAccessControlAllowCredentials = "Access-Control-Allow-Credentials"
	HeaderAccessControlExposeHeaders    = "Access-Control-Expose-Headers"
	HeaderAccessControlMaxAge           = "Access-Control-Max-Age"

	// Security Headers
	HeaderStrictTransportSecurity = "Strict-Transport-Security"
	HeaderXContentTypeOptions     = "X-Content-Type-Options"
	HeaderXFrameOptions           = "X-Frame-Options"
	HeaderContentSecurityPolicy   = "Content-Security-Policy"
	HeaderXXSSProtection          = "X-XSS-Protection"
	HeaderReferrerPolicy          = "Referrer-Policy"

	// HTTP/3 Specific Headers
	HeaderAltSvc    = "Alt-Svc"
	HeaderEarlyData = "Early-Data"
	HeaderPriority  = "Priority"
)

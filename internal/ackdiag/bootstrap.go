package ackdiag

// Bootstrap observations never decide whether production accepts a response.
// Only closed enums, counts and booleans can leave this file.
import (
 "bytes"
 "compress/flate"
 "compress/gzip"
 "encoding/json"
 "errors"
 "io"
 "mime"
 "net"
 "net/http"
 "net/url"
 "regexp"
 "strings"
 "syscall"

 "golang.org/x/net/html"
)

type BootstrapResponse struct {
 BootstrapStructure
 Schema int `json:"schema"`
 Event string `json:"event"`
 Ordinal uint64 `json:"ordinal"`
 HTTPStatusCode int `json:"http_status_code"`
 HTTPStatusClass string `json:"http_status_class"`
 ContentTypeClass string `json:"content_type_class"`
 ContentEncodingClass string `json:"content_encoding_class"`
 BodyBytesRead uint64 `json:"body_bytes_read"`
 BodyReadComplete bool `json:"body_read_complete"`
 BodyReadErrorClass string `json:"body_read_error_class"`
 FinalRouteClass string `json:"final_route_class"`
 ResponseResult string `json:"response_result"`
 ClientConfigSearched bool `json:"client_config_searched"`
 ClientConfigMatched bool `json:"client_config_matched"`
 ClientConfigMatchCount uint64 `json:"client_config_match_count"`
 ClientConfigMatchCountCapped bool `json:"client_config_match_count_capped"`
 ClientConfigParseResult string `json:"client_config_parse_result"`
 ClientConfigSearchOnNon2XX bool `json:"client_config_search_on_non2xx"`
 IsHTML bool `json:"is_html"`
 HasClientConfigPattern bool `json:"has_client_config_pattern"`
 HasExpectedDocsBootstrapStructure bool `json:"has_expected_docs_bootstrap_structure"`
 HasKnownAuthStructure bool `json:"has_known_auth_structure"`
 HasKnownChallengeStructure bool `json:"has_known_challenge_structure"`
 HasKnownPermissionErrorStructure bool `json:"has_known_permission_error_structure"`
 StructureScanComplete bool `json:"structure_scan_complete"`
}

func readErrorClass(err error) string {
 if err==nil{return "NONE"}
 if errors.Is(err,gzip.ErrHeader)||errors.Is(err,gzip.ErrChecksum){return "DECOMPRESSION_ERROR"}
 var corrupt flate.CorruptInputError
 if errors.As(err,&corrupt){return "DECOMPRESSION_ERROR"}
 if errors.Is(err,io.ErrUnexpectedEOF){return "UNEXPECTED_EOF"}
 var timeout net.Error
 if errors.As(err,&timeout)&&timeout.Timeout(){return "TIMEOUT"}
 if errors.Is(err,syscall.ECONNRESET){return "CONNECTION_RESET"}
 return "OTHER_IO_ERROR"
}

func statusClass(code int) string {
 switch {case code>=200&&code<300:return "2XX";case code>=300&&code<400:return "3XX";case code>=400&&code<500:return "4XX";case code>=500&&code<600:return "5XX"};return "OTHER"
}
func contentTypeClass(value string) string {
 if value==""{return "MISSING"}
 t,_,err:=mime.ParseMediaType(value);if err!=nil{return "INVALID"}
 switch strings.ToLower(t){case "text/html","application/xhtml+xml":return "HTML";case "application/json":return "JSON"}
 if strings.HasPrefix(strings.ToLower(t),"text/"){return "TEXT"};return "OTHER"
}
func encodingClass(resp *http.Response) string {
 if resp.Uncompressed{return "GZIP_AUTO_DECODED"}
 switch strings.ToLower(strings.TrimSpace(resp.Header.Get("Content-Encoding"))){case "","identity":return "IDENTITY";case "gzip":return "GZIP_ENCODED";case "br":return "BR_ENCODED";case "deflate":return "DEFLATE_ENCODED"};return "OTHER_ENCODED"
}
func providerHost(h string) bool {return h=="yandex.ru"||h=="yandex.com"||strings.HasSuffix(h,".yandex.ru")||strings.HasSuffix(h,".yandex.com")}
func routeClass(raw string,challenge bool) string {
 u,err:=url.Parse(raw);if err!=nil||u.Hostname()==""||(u.Scheme!="https"&&u.Scheme!="http"){return "UNKNOWN_ROUTE"}
 h:=strings.ToLower(u.Hostname());p:=strings.ToLower(u.Path)
 if !providerHost(h){return "UNEXPECTED_HOST_CLASS"}
 if challenge{return "YANDEX_CAPTCHA_OR_CHALLENGE"}
 if strings.HasPrefix(h,"passport.")||strings.HasPrefix(h,"id.")||p=="/auth"||p=="/login"||strings.HasPrefix(p,"/auth/")||strings.HasPrefix(p,"/login/"){return "YANDEX_AUTH"}
 if p=="/document/error/"||p=="/document/error"||p=="/access-denied"{return "YANDEX_PERMISSION_OR_ERROR"}
 if h=="docs.yandex.ru"||h=="docs.yandex.com"{return "YANDEX_DOCS"}
 return "OTHER_YANDEX"
}

// Bounded structural hints. A matching marker is evidence of that structure,
// never proof of page authenticity or a provider policy. Absence proves nothing.
func inspectStructure(e *BootstrapResponse,body []byte) {
 const limit=1024*1024
 e.StructureScanComplete=len(body)<=limit
 if len(body)>limit{body=body[:limit]}
 z:=html.NewTokenizer(bytes.NewReader(body));z.SetMaxBuf(1024*1024+1)
 challengeForm,captchaInput,smartWidget,smartScript:=false,false,false,false
 loginForm,passwordInput:=false,false
 for tokens:=0;;tokens++ {
  tt:=z.Next();if tt==html.ErrorToken{if z.Err()!=io.EOF{e.StructureScanComplete=false};break}
  if tokens>=StructureTokenLimit{e.StructureScanComplete=false;break}
  if tt!=html.StartTagToken&&tt!=html.SelfClosingTagToken&&tt!=html.DoctypeToken{continue}
  tok:=z.Token();if tok.Data=="html"||tt==html.DoctypeToken{e.IsHTML=true}
  attrs:=map[string]string{};for _,a:=range tok.Attr{attrs[a.Key]=a.Val}
  if tok.Data=="form" {
   u,err:=url.Parse(attrs["action"])
   if err==nil&&(u.Hostname()==""||providerHost(strings.ToLower(u.Hostname()))) {
    if u.Path=="/checkcaptcha"||u.Path=="/showcaptcha"{challengeForm=true}
    if routeClass(attrs["action"],false)=="YANDEX_AUTH"{loginForm=true}
   }
  }
  if tok.Data=="input"&&attrs["name"]=="rep"{captchaInput=true}
  if tok.Data=="input"&&strings.EqualFold(attrs["type"],"password"){passwordInput=true}
  if tok.Data=="script" {
   if attrs["id"]=="client-config"{e.HasExpectedDocsBootstrapStructure=true}
   u,err:=url.Parse(attrs["src"]);if err==nil&&u.Hostname()=="smartcaptcha.yandexcloud.net"&&u.Path=="/captcha.js"{smartScript=true}
  }
  for _,c:=range strings.Fields(attrs["class"]){if c=="smart-captcha"&&attrs["data-sitekey"]!=""{smartWidget=true}}
  if attrs["role"]=="alert"&&(attrs["data-error-code"]=="access_denied"||attrs["data-error-code"]=="permission_denied"||attrs["data-error-code"]=="document_not_found"){e.HasKnownPermissionErrorStructure=true}
 }
 e.HasKnownChallengeStructure=(challengeForm&&captchaInput)||(smartWidget&&smartScript)
 e.HasKnownAuthStructure=loginForm&&passwordInput
}

func newBootstrapResponse(raw string,resp *http.Response,body []byte,readErr error,pattern *regexp.Regexp) *BootstrapResponse {
 e:=&BootstrapResponse{Schema:LogSchema,Event:"bootstrap_response",HTTPStatusCode:resp.StatusCode,HTTPStatusClass:statusClass(resp.StatusCode),ContentTypeClass:contentTypeClass(resp.Header.Get("Content-Type")),ContentEncodingClass:encodingClass(resp),BodyBytesRead:uint64(len(body)),BodyReadComplete:readErr==nil,BodyReadErrorClass:readErrorClass(readErr),ClientConfigParseResult:"NOT_ATTEMPTED"}
 e.IsHTML=e.ContentTypeClass=="HTML"
 if e.ContentEncodingClass=="IDENTITY"||e.ContentEncodingClass=="GZIP_AUTO_DECODED"{inspectStructure(e,body)}
 e.FinalRouteClass=routeClass(raw,e.HasKnownChallengeStructure)
 inspectBootstrapStructure(e,raw,body)
 // Same exact production expression; bounded result allocation only. Count=64
 // with capped=true means at least 65 matches, not an invented exact count.
 matches:=pattern.FindAllIndex(body,65);e.ClientConfigMatchCount=uint64(len(matches))
 if len(matches)>64{e.ClientConfigMatchCount=64;e.ClientConfigMatchCountCapped=true}
 e.HasClientConfigPattern=len(matches)>0
 e.ResponseResult=bootstrapResult(e)
 return e
}
func bootstrapResult(e *BootstrapResponse) string {
 if !e.BodyReadComplete{if e.BodyBytesRead>0{return "BODY_READ_PARTIAL"};return "BODY_READ_FAILED"}
 if e.ContentEncodingClass!="IDENTITY"&&e.ContentEncodingClass!="GZIP_AUTO_DECODED"{return "UNSUPPORTED_ENCODING"}
 if e.HTTPStatusClass=="3XX"{return "REDIRECT"}
 if e.BodyBytesRead==0{return "EMPTY_BODY"}
 if e.ClientConfigMatched{return "CLIENT_CONFIG_FOUND"}
 if e.ClientConfigSearched{if e.HTTPStatusClass=="2XX"{return "CLIENT_CONFIG_NOT_FOUND_2XX"};return "CLIENT_CONFIG_NOT_FOUND_NON2XX"}
 return "UNKNOWN_RESPONSE"
}
func ObserveBootstrapResponse(raw string,resp *http.Response,body []byte,err error,pattern *regexp.Regexp) *BootstrapResponse {
 if startupRecorder.Load()==nil{return nil}
 return newBootstrapResponse(raw,resp,body,err,pattern)
}
func ObserveBootstrapTerminal(result,raw string,err error) *BootstrapResponse {
 if startupRecorder.Load()==nil{return nil}
 if result!="NETWORK_ERROR"&&result!="REDIRECT_LIMIT"{result="UNKNOWN_RESPONSE"}
 e:=&BootstrapResponse{Schema:LogSchema,Event:"bootstrap_response",HTTPStatusClass:"OTHER",ContentTypeClass:"MISSING",ContentEncodingClass:"IDENTITY",BodyReadErrorClass:readErrorClass(err),FinalRouteClass:routeClass(raw,false),ResponseResult:result,ClientConfigParseResult:"NOT_ATTEMPTED"}
 initBootstrapStructure(e,raw)
 e.StructureScanLimitReason="OTHER"
 return e
}
func BootstrapSearch(e *BootstrapResponse,matched bool) {
 if e==nil{return};e.ClientConfigSearched=true;e.ClientConfigMatched=matched;e.ClientConfigSearchOnNon2XX=e.HTTPStatusClass!="2XX";e.ResponseResult=bootstrapResult(e)
}
func BootstrapParsed(e *BootstrapResponse,err error) {
 if e==nil{return};e.ClientConfigParseResult="OK";if err!=nil{e.ClientConfigParseResult="ERROR"}
}
func EmitBootstrap(e *BootstrapResponse) {
 if e==nil{return};r:=startupRecorder.Load();if r==nil{return}
 r.mu.Lock();defer r.mu.Unlock()
 if r.failed{return}
 r.bootstrapChallenge=e.HasKnownChallengeStructure&&e.FinalRouteClass=="YANDEX_CAPTCHA_OR_CHALLENGE"
 r.bootstrapOrdinal++
 if r.bootstrapOrdinal>11 {r.failed=true;r.state.Ordinal++;r.state.Result="failure";r.state.Stage=Authorization;r.state.Failure=SchemaFailure;writeStartup(r.out,r.state);return}
 e.Schema=LogSchema;e.Event="bootstrap_response";e.Ordinal=r.bootstrapOrdinal
 b,err:=json.Marshal(e)
 if err==nil{line:=append(append([]byte("[ACK-BOOTSTRAP] "),b...), '\n');var n int;if r.out==nil{err=io.ErrClosedPipe}else{n,err=r.out.Write(line)};if n!=len(line)&&err==nil{err=io.ErrShortWrite}}
 if err!=nil{r.failed=true;r.state.Ordinal++;r.state.Result="failure";r.state.Stage=Authorization;r.state.Failure=SchemaFailure;writeStartup(r.fallback,r.state)}
}

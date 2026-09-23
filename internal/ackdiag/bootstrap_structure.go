package ackdiag

// Observation only. No HTTP client, logging of inputs, or authorization output.
import (
 "bytes"
 "encoding/json"
 "errors"
 "io"
 "mime"
 "net/url"
 "strings"

 "golang.org/x/net/html"
)

const (
 StructureHTMLLimit = 1024*1024
 StructureTokenLimit = 65536
 StructureScriptLimit = 1024
 StructureJSONLimit = 768*1024 // total inspected inline JSON bytes per response
 StructureDepthLimit = 64 // JSON; token nesting has a separate 256 bound
 StructureHTMLDepthLimit = 256
)

// Named structs, not arbitrary extension maps. Only closed enums leave memory.
type StaticResourceFamilies struct {
 PSF uint64 `json:"YANDEX_STATIC_PSF"`
 Docs uint64 `json:"YANDEX_STATIC_DOCS"`
 Generic uint64 `json:"YANDEX_STATIC_GENERIC"`
 OtherYandex uint64 `json:"OTHER_YANDEX_STATIC"`
 ThirdParty uint64 `json:"THIRD_PARTY_STATIC"`
 Relative uint64 `json:"RELATIVE_STATIC"`
 Unknown uint64 `json:"UNKNOWN_STATIC_FAMILY"`
}
type DocsAppMarkers struct {
 Legacy bool `json:"legacy_client_config"`
 InitialStore bool `json:"public_initial_store"`
 Editor bool `json:"editor_root"`
 Generic bool `json:"generic_root"`
}
type BootstrapStructure struct {
 FinalPathClass string `json:"final_path_class"`
 ScriptTotalCount uint64 `json:"script_total_count"`
 ScriptInlineCount uint64 `json:"script_inline_count"`
 ScriptExternalCount uint64 `json:"script_external_count"`
 ScriptJSONCount uint64 `json:"script_json_count"`
 ScriptModuleCount uint64 `json:"script_module_count"`
 FormCount uint64 `json:"form_count"`
 IframeCount uint64 `json:"iframe_count"`
 NoscriptCount uint64 `json:"noscript_count"`
 StylesheetCount uint64 `json:"stylesheet_count"`
 RootContainerClass string `json:"root_container_class"`
 InitialStorePresent bool `json:"initial_store_present"`
 InitialStoreStructureClass string `json:"initial_store_structure_class"`
 OtherInlineBootstrapPresent bool `json:"other_inline_bootstrap_present"`
 OtherInlineBootstrapStructureClass string `json:"other_inline_bootstrap_structure_class"`
 ExpectedDocsAppMarkers DocsAppMarkers `json:"expected_docs_app_markers"`
 BrowserCompatibilityMarker string `json:"browser_compatibility_marker"`
 StaticResourceFamilyClasses StaticResourceFamilies `json:"static_resource_family_classes"`
 StructureScanLimitReason string `json:"structure_scan_limit_reason"`
 RequiredAuthFieldShapePresent bool `json:"required_auth_field_shape_present"`
 OptionalTTLFieldPresent bool `json:"optional_ttl_field_present"`
 CandidateContainerCount uint64 `json:"candidate_container_count"`
 StructuralClassificationConflict bool `json:"structural_classification_conflict"`
 PageStructureClass string `json:"page_structure_class"`
}

func finalPathClass(raw string) string {
 u,err:=url.Parse(raw)
 if err!=nil||u.User!=nil||(u.Scheme!="https"&&u.Scheme!="http")||!providerHost(strings.ToLower(u.Hostname())) {return "UNKNOWN_PATH_CLASS"}
 h,p:=strings.ToLower(u.Hostname()),strings.ToLower(u.Path)
 // These are route hints only, never CAPTCHA or authorization evidence.
 if p=="/showcaptcha"||p=="/checkcaptcha" {return "YANDEX_CHALLENGE_PATH_CLASS"}
 if strings.HasPrefix(h,"passport.")||strings.HasPrefix(h,"id.")||pathFamily(p,"auth")||pathFamily(p,"login") {return "YANDEX_AUTH_PATH_CLASS"}
 if h!="docs.yandex.ru"&&h!="docs.yandex.com" {return "OTHER_YANDEX_PATH_CLASS"}
 switch {
 case p==""||p=="/":return "DOCS_ROOT"
 case pathFamily(p,"document/error")||pathFamily(p,"error")||pathFamily(p,"access-denied"):return "DOCS_ERROR_LIKE"
 case pathFamily(p,"edit")||pathFamily(p,"document"):return "DOCS_EDITOR_LIKE"
 case pathFamily(p,"view")||pathFamily(p,"viewer"):return "DOCS_VIEWER_LIKE"
 case pathFamily(p,"i")||pathFamily(p,"d")||pathFamily(p,"share"):return "DOCS_PUBLIC_SHARE_LIKE"
 default:return "DOCS_UNKNOWN_PATH_CLASS"
 }
}
func pathFamily(p, family string) bool {return p=="/"+family||strings.HasPrefix(p,"/"+family+"/")}

func (f *StaticResourceFamilies) observe(raw string) {
 u,err:=url.Parse(strings.TrimSpace(raw))
 if err!=nil||raw==""||u.User!=nil||(u.Scheme!=""&&u.Scheme!="https"&&u.Scheme!="http") {f.Unknown++;return}
 h:=strings.ToLower(u.Hostname())
 if h=="" {f.Relative++;return}
 switch {
 case h=="yastatic.net"&&strings.HasPrefix(u.Path,"/s3/psf/"):f.PSF++
 case h=="yastatic.net"&&(strings.HasPrefix(u.Path,"/s3/docs/")||strings.HasPrefix(u.Path,"/s3/docviewer/")):f.Docs++
 case h=="yastatic.net":f.Generic++
 case providerHost(h)||h=="yandex.net"||strings.HasSuffix(h,".yandex.net")||h=="smartcaptcha.yandexcloud.net":f.OtherYandex++
 default:f.ThirdParty++
 }
 // raw/u are local only. No URL, fragment, dynamic suffix, or digest is retained.
}

func initBootstrapStructure(e *BootstrapResponse,raw string) {
 e.BootstrapStructure=BootstrapStructure{FinalPathClass:finalPathClass(raw),RootContainerClass:"NONE",
 InitialStoreStructureClass:"ABSENT",OtherInlineBootstrapStructureClass:"ABSENT",
 BrowserCompatibilityMarker:"UNKNOWN",StructureScanLimitReason:"NONE",PageStructureClass:"UNKNOWN_DOCS_HTML"}
}

var errJSONDepth=errors.New("depth")
var errJSONMalformed=errors.New("malformed")
// Token-by-token decoding bounds nesting before object allocation. Duplicate
// keys and trailing values are ambiguous and rejected, not silently overwritten.
func readJSONValue(d *json.Decoder,depth int) (any,error) {
 if depth>StructureDepthLimit{return nil,errJSONDepth}
 token,err:=d.Token();if err!=nil{return nil,errJSONMalformed}
 delim,ok:=token.(json.Delim);if !ok{return token,nil}
 switch delim {
 case '{':
  obj:=make(map[string]any)
  for d.More(){key,err:=d.Token();if err!=nil{return nil,errJSONMalformed};s,ok:=key.(string);if !ok{return nil,errJSONMalformed};if _,exists:=obj[s];exists{return nil,errJSONMalformed};v,err:=readJSONValue(d,depth+1);if err!=nil{return nil,err};obj[s]=v}
  end,err:=d.Token();if err!=nil||end!=json.Delim('}'){return nil,errJSONMalformed};return obj,nil
 case '[':
  values:=[]any{}
  for d.More(){v,err:=readJSONValue(d,depth+1);if err!=nil{return nil,err};values=append(values,v)}
  end,err:=d.Token();if err!=nil||end!=json.Delim(']'){return nil,errJSONMalformed};return values,nil
 default:return nil,errJSONMalformed
 }
}
func decodeStructureJSON(b []byte) (any,error) {
 d:=json.NewDecoder(bytes.NewReader(b));d.UseNumber()
 v,err:=readJSONValue(d,0);if err!=nil{return nil,err}
 if _,err=d.Token();err!=io.EOF{return nil,errJSONMalformed};return v,nil
}
func nonemptyAuthScalar(v any) bool {
 switch x:=v.(type){case string:return x!="";case json.Number:return x.String()!=""};return false
}
func requiredShape(v any) (found,ttl bool) {
 switch x:=v.(type) {
 case map[string]any:
  if o,ok:=x["officeActionData"].(map[string]any);ok&&nonemptyAuthScalar(o["action_url"])&&nonemptyAuthScalar(o["access_token"]) {found=true;_,ttl=o["access_token_ttl"]}
  for _,child:=range x {f,t:=requiredShape(child);found=found||f;ttl=ttl||t}
 case []any:for _,child:=range x {f,t:=requiredShape(child);found=found||f;ttl=ttl||t}
 }
 return
}
func landingShape(v any) bool {
 m,ok:=v.(map[string]any);if !ok{return false}
 _,router:=m["router"].(map[string]any);_,env:=m["environment"].(map[string]any)
 _,subscription:=m["subscription"];_,tariffs:=m["tariffs"];_,onboarding:=m["onboarding"]
 return router&&env&&subscription&&tariffs&&onboarding
}
func bootstrapHint(v any) bool {
 m,ok:=v.(map[string]any);if !ok{return false}
 for _,key:=range []string{"officeActionData","editorParams","initialState","bootstrap","environment","router"}{if _,ok:=m[key];ok{return true}}
 return false
}

type structureScanner struct {
 e *BootstrapResponse
 roots, initial, other int
 initialRequired, otherRequired, landing bool
 jsonBytes int
 browserContainerDepth int
 browserMarker bool
}
func (s *structureScanner) limit(reason string) {if s.e.StructureScanLimitReason=="NONE"{s.e.StructureScanLimitReason=reason};s.e.StructureScanComplete=false}
func (s *structureScanner) candidate(id string,b []byte) {
 e:=s.e;e.CandidateContainerCount++
 if id=="initial-store" {s.initial++;e.InitialStorePresent=true} else if id!="client-config" {s.other++;e.OtherInlineBootstrapPresent=true}
 if len(b)>StructureJSONLimit-s.jsonBytes {s.limit("INLINE_JSON_SIZE");if id=="initial-store"{e.InitialStoreStructureClass="UNKNOWN_SHAPE"}else if id!="client-config"{e.OtherInlineBootstrapStructureClass="UNKNOWN"};return}
 s.jsonBytes+=len(b)
 v,err:=decodeStructureJSON(b)
 if errors.Is(err,errJSONDepth){s.limit("DEPTH")}
 if err!=nil {if id=="initial-store"{e.InitialStoreStructureClass="MALFORMED"}else if id!="client-config"{e.OtherInlineBootstrapStructureClass="MALFORMED"};return}
 found,ttl:=requiredShape(v);e.RequiredAuthFieldShapePresent=e.RequiredAuthFieldShapePresent||found;e.OptionalTTLFieldPresent=e.OptionalTTLFieldPresent||ttl
 if id=="initial-store" {
  s.initialRequired=s.initialRequired||found;s.landing=s.landing||landingShape(v)
  switch {case landingShape(v)&&found:s.e.StructuralClassificationConflict=true;e.InitialStoreStructureClass="UNKNOWN_SHAPE"
  case found:e.InitialStoreStructureClass="EDITOR_COMPATIBLE_REQUIRED_FIELDS_PRESENT"
  case landingShape(v):e.InitialStoreStructureClass="PUBLIC_LANDING_KNOWN_SHAPE"
  default:if _,ok:=v.(map[string]any);ok{e.InitialStoreStructureClass="EDITOR_REQUIRED_FIELDS_ABSENT"}else{e.InitialStoreStructureClass="UNKNOWN_SHAPE"}}
 } else if id!="client-config" {
  s.otherRequired=s.otherRequired||found
  switch {case found:e.OtherInlineBootstrapStructureClass="REQUIRED_OPENFLUX_FIELDS_PRESENT"
  case bootstrapHint(v):e.OtherInlineBootstrapStructureClass="POSSIBLE_BOOTSTRAP_WITHOUT_REQUIRED_FIELDS"
  default:e.OtherInlineBootstrapStructureClass="UNKNOWN"}
 }
}

func inspectBootstrapStructure(e *BootstrapResponse,raw string,body []byte) {
 legacyComplete:=e.StructureScanComplete
 initBootstrapStructure(e,raw)
 s:=&structureScanner{e:e,browserContainerDepth:-1}
 if e.ContentEncodingClass!="IDENTITY"&&e.ContentEncodingClass!="GZIP_AUTO_DECODED" {s.limit("OTHER");return}
 e.StructureScanComplete=true
 if len(body)>StructureHTMLLimit {s.limit("HTML_SIZE");body=body[:StructureHTMLLimit]}
 z:=html.NewTokenizer(bytes.NewReader(body));z.SetMaxBuf(StructureHTMLLimit+1)
 // Nesting tracks structural elements only; void/self-closing elements do not
 // accumulate depth. It is a scan budget, not browser DOM reconstruction.
 stack:=[]string{}
 var jsonID string
 jsonPending:=false
 var jsonBody []byte
 for tokens:=0;;tokens++ {
  tt:=z.Next()
  if tt==html.ErrorToken {if z.Err()!=io.EOF{s.limit("OTHER")};if jsonPending{s.candidate(jsonID,jsonBody);s.limit("OTHER")};break}
  if tokens>=StructureTokenLimit{s.limit("TOKEN_COUNT");break}
  if tt==html.TextToken&&jsonPending {
   part:=z.Text();if len(part)>StructureJSONLimit-s.jsonBytes-len(jsonBody){s.limit("INLINE_JSON_SIZE");break};jsonBody=append(jsonBody,part...);continue
  }
  if tt==html.EndTagToken {
   name,_:=z.TagName();tag:=string(name)
   if tag=="script"&&jsonPending {s.candidate(jsonID,jsonBody);jsonPending=false;jsonBody=nil;if e.StructureScanLimitReason!="NONE"{break}}
   for i:=len(stack)-1;i>=0;i-- {if stack[i]==tag{stack=stack[:i];break}}
   if s.browserContainerDepth>=len(stack){s.browserContainerDepth=-1}
   continue
  }
  if tt!=html.StartTagToken&&tt!=html.SelfClosingTagToken {continue}
  tok:=z.Token();attrs:=map[string]string{};for _,a:=range tok.Attr{if _,exists:=attrs[a.Key];exists{s.limit("OTHER")};attrs[a.Key]=a.Val}
  if (tok.Data=="main"||tok.Data=="div")&&attrs["id"]=="unsupported-browser" {s.browserContainerDepth=len(stack)}
  if tok.Data=="a"&&s.browserContainerDepth>=0&&attrs["data-action"]=="update-browser" {s.browserMarker=true}
  if tok.Data!="script"&&tok.Data!="style" {
   switch attrs["id"] {case "root","app":s.roots++;e.ExpectedDocsAppMarkers.Generic=true
   case "editor","document-editor":s.roots++;e.ExpectedDocsAppMarkers.Editor=true}
  }
  switch tok.Data {
  case "form":e.FormCount++
  case "iframe":e.IframeCount++
  case "noscript":e.NoscriptCount++
  case "link":for _,rel:=range strings.Fields(strings.ToLower(attrs["rel"])){if rel=="stylesheet"{e.StylesheetCount++;break}}
  case "script":
   if e.ScriptTotalCount>=StructureScriptLimit{s.limit("SCRIPT_COUNT");break}
   e.ScriptTotalCount++
   _,external:=attrs["src"]
   if external{e.ScriptExternalCount++;e.StaticResourceFamilyClasses.observe(attrs["src"])}else{e.ScriptInlineCount++}
   kind,_,err:=mime.ParseMediaType(attrs["type"]);isJSON:=err==nil&&strings.EqualFold(kind,"application/json")
   if isJSON{e.ScriptJSONCount++};if strings.EqualFold(strings.TrimSpace(attrs["type"]),"module"){e.ScriptModuleCount++}
   if attrs["id"]=="client-config"{e.ExpectedDocsAppMarkers.Legacy=true}
   if attrs["id"]=="initial-store"{e.InitialStorePresent=true;e.ExpectedDocsAppMarkers.InitialStore=true;e.InitialStoreStructureClass="UNKNOWN_SHAPE"}
   if !external&&(isJSON||attrs["id"]=="client-config"||attrs["id"]=="initial-store") {
    jsonPending=true;jsonID=attrs["id"];jsonBody=nil
   }
  }
  if e.StructureScanLimitReason!="NONE" {break}
  if tt!=html.SelfClosingTagToken&&!voidElement(tok.Data) {stack=append(stack,tok.Data);if len(stack)>StructureHTMLDepthLimit{s.limit("DEPTH");break}}
 }
 if !legacyComplete&&e.StructureScanLimitReason=="NONE" {s.limit("OTHER")}
 if s.initial>1{e.InitialStoreStructureClass="UNKNOWN_SHAPE";e.StructuralClassificationConflict=true}
 if s.other>1{e.OtherInlineBootstrapStructureClass="MULTIPLE_CANDIDATES";e.StructuralClassificationConflict=true}
 if !e.BodyReadComplete{s.limit("OTHER")}
 if !e.StructureScanComplete {if e.InitialStorePresent&&e.InitialStoreStructureClass=="ABSENT"{e.InitialStoreStructureClass="UNKNOWN_SHAPE"}}
 switch {case s.roots>1:e.RootContainerClass="MULTIPLE_ROOTS"
 case e.ExpectedDocsAppMarkers.Editor:e.RootContainerClass="KNOWN_EDITOR_ROOT"
 case e.ExpectedDocsAppMarkers.Generic&&e.InitialStoreStructureClass=="PUBLIC_LANDING_KNOWN_SHAPE":e.RootContainerClass="KNOWN_PUBLIC_DOCS_ROOT"
 case e.ExpectedDocsAppMarkers.Generic:e.RootContainerClass="GENERIC_APP_ROOT"}
 if s.browserMarker {e.BrowserCompatibilityMarker="YES"}
 finishStructure(e,s.initialRequired||s.otherRequired,s.landing)
}
func voidElement(s string) bool {switch s{case "area","base","br","col","embed","hr","img","input","link","meta","param","source","track","wbr":return true};return false}

func finishStructure(e *BootstrapResponse,alternative,landing bool) {
 // Every positive class participates. Never hide conflicting auth/editor hints.
 classes:=[]string{}
 add:=func(ok bool,c string){if ok{classes=append(classes,c)}}
 add(e.ExpectedDocsAppMarkers.Legacy,"LEGACY_EDITOR_CLIENT_CONFIG")
 add(landing,"PUBLIC_LANDING_INITIAL_STORE")
 add(alternative,"EDITOR_LIKE_INLINE_BOOTSTRAP")
 add(e.StructureScanComplete&&e.ExpectedDocsAppMarkers.Editor&&!e.RequiredAuthFieldShapePresent&&!e.ExpectedDocsAppMarkers.Legacy&&!landing,"EDITOR_LIKE_NO_REQUIRED_BOOTSTRAP")
 add(e.BrowserCompatibilityMarker=="YES","BROWSER_COMPATIBILITY")
 add(e.HasKnownAuthStructure,"AUTH_PAGE");add(e.HasKnownChallengeStructure,"CHALLENGE_PAGE");add(e.HasKnownPermissionErrorStructure,"PERMISSION_ERROR_PAGE")
 e.StructuralClassificationConflict=e.StructuralClassificationConflict||len(classes)>1
 switch {
 case e.StructuralClassificationConflict:e.PageStructureClass="AMBIGUOUS_STRUCTURE"
 case !e.StructureScanComplete:e.PageStructureClass="UNKNOWN_DOCS_HTML"
 case len(classes)==1:e.PageStructureClass=classes[0]
 case e.ExpectedDocsAppMarkers.Generic:e.PageStructureClass="GENERIC_DOCS_SHELL"
 default:e.PageStructureClass="UNKNOWN_DOCS_HTML"
 }
 if e.BrowserCompatibilityMarker!="YES"&&e.StructureScanComplete&&e.PageStructureClass!="UNKNOWN_DOCS_HTML"&&e.PageStructureClass!="AMBIGUOUS_STRUCTURE"{e.BrowserCompatibilityMarker="NO"}
}

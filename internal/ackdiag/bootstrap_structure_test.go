package ackdiag

import (
 "encoding/json"
 "os"
 "path/filepath"
 "strings"
 "testing"
)

const requiredJSON=`{"officeActionData":{"action_url":"https://private.invalid/FAKE_DOC_39481?secret=FAKE_QUERY_7863","access_token":"FAKE_ACCESS_99817","access_token_ttl":123}}`
const landingJSON=`{"router":{},"environment":{},"subscription":{},"tariffs":{},"onboarding":{}}`
// Fixed structural contract, not a captured provider body. Matching this fixture
// does not prove the previous user3 response had the same compatibility markup.
const browserFixture=`<main id="unsupported-browser"><a data-action="update-browser">FAKE_BODY_SECRET</a></main>`

type structureCase struct {
 name,body,page,limit string
 total,inline,external,jsons,modules uint64
 required,ttl bool
}
func structureCases() []structureCase {return []structureCase{
 {name:"legacy",body:`<html><script id="client-config">`+requiredJSON+`</script></html>`,page:"LEGACY_EDITOR_CLIENT_CONFIG",total:1,inline:1,required:true,ttl:true},
 {name:"public_root",body:`<div id="root"></div><script type="application/json" id="initial-store">`+landingJSON+`</script><script src="https://yastatic.net/s3/psf/app.js"></script>`,page:"PUBLIC_LANDING_INITIAL_STORE",total:2,inline:1,external:1,jsons:1},
 {name:"public_reordered",body:`<script src="https://yastatic.net/s3/psf/app.js"></script><script id="initial-store" type="application/json">`+landingJSON+`</script><div id="root"></div>`,page:"PUBLIC_LANDING_INITIAL_STORE",total:2,inline:1,external:1,jsons:1},
 {name:"editor_required",body:`<main id="editor"></main><script type="application/json">`+requiredJSON+`</script>`,page:"EDITOR_LIKE_INLINE_BOOTSTRAP",total:1,inline:1,jsons:1,required:true,ttl:true},
 {name:"editor_empty",body:`<div id="document-editor"></div>`,page:"EDITOR_LIKE_NO_REQUIRED_BOOTSTRAP"},
 {name:"other_json",body:`<script id="FAKE_DYNAMIC_ID" type="application/json">{"state":[`+requiredJSON+`]}</script>`,page:"EDITOR_LIKE_INLINE_BOOTSTRAP",total:1,inline:1,jsons:1,required:true,ttl:true},
 {name:"multiple_json",body:`<script type="application/ld+json">{"secret":"FAKE_JSON_SECRET"}</script><script type="application/json">`+requiredJSON+`</script>`,page:"EDITOR_LIKE_INLINE_BOOTSTRAP",total:2,inline:2,jsons:1,required:true,ttl:true},
 {name:"multiple_candidates",body:`<script type="application/json">`+requiredJSON+`</script><script type="application/json">{"environment":{}}</script>`,page:"AMBIGUOUS_STRUCTURE",total:2,inline:2,jsons:2,required:true,ttl:true},
 {name:"browser",body:browserFixture,page:"BROWSER_COMPATIBILITY"},
 {name:"challenge",body:challengeFixture,page:"CHALLENGE_PAGE"},
 {name:"auth",body:`<form action="https://passport.yandex.ru/auth?secret=FAKE_QUERY_7863"><input type=password></form>`,page:"AUTH_PAGE"},
 {name:"permission",body:`<div role=alert data-error-code=access_denied>FAKE_BODY_SECRET</div>`,page:"PERMISSION_ERROR_PAGE"},
 {name:"generic",body:`<html><div id=app></div></html>`,page:"GENERIC_DOCS_SHELL"},
 {name:"unknown",body:`<html><p>FAKE_BODY_SECRET</p></html>`,page:"UNKNOWN_DOCS_HTML"},
 {name:"no_scripts",body:`<html><link rel=stylesheet href="https://private.invalid/secret.css"><form></form><iframe></iframe><noscript></noscript></html>`,page:"UNKNOWN_DOCS_HTML"},
 {name:"external_only",body:`<script src="/asset/FAKE_DOC_39481.js?token=FAKE_ACCESS_99817"></script>`,page:"UNKNOWN_DOCS_HTML",total:1,external:1},
 {name:"modules_only",body:`<script type=module>FAKE_SCRIPT_SECRET</script><script type=module src="https://yastatic.net/app.js"></script>`,page:"UNKNOWN_DOCS_HTML",total:2,inline:1,external:1,modules:2},
 {name:"malformed_json",body:`<script type=application/json>{"secret":"FAKE_JSON_SECRET"</script>`,page:"UNKNOWN_DOCS_HTML",total:1,inline:1,jsons:1},
 {name:"giant_json",body:`<script id=initial-store type=application/json>{"secret":"`+strings.Repeat("x",StructureJSONLimit)+`"}</script>`,page:"UNKNOWN_DOCS_HTML",limit:"INLINE_JSON_SIZE",total:1,inline:1,jsons:1},
 {name:"excess_scripts",body:strings.Repeat(`<script></script>`,StructureScriptLimit+1),page:"UNKNOWN_DOCS_HTML",limit:"SCRIPT_COUNT",total:StructureScriptLimit,inline:StructureScriptLimit},
 {name:"excess_html",body:strings.Repeat("x",StructureHTMLLimit+1),page:"UNKNOWN_DOCS_HTML",limit:"HTML_SIZE"},
 {name:"excess_tokens",body:strings.Repeat(`<b></b>`,StructureTokenLimit/2+1),page:"UNKNOWN_DOCS_HTML",limit:"TOKEN_COUNT"},
 {name:"conflict",body:challengeFixture+`<div id=editor></div><script type=application/json>`+requiredJSON+`</script>`,page:"AMBIGUOUS_STRUCTURE",total:1,inline:1,jsons:1,required:true,ttl:true},
 {name:"third_party",body:`<script src="https://private-host.invalid/FAKE_DOC_39481?token=FAKE_QUERY_7863#FAKE_FRAGMENT_928"></script>`,page:"UNKNOWN_DOCS_HTML",total:1,external:1},
 {name:"document_path",body:`<script src="https://yastatic.net/s3/docs/FAKE_DOC_39481/app.js"></script>`,page:"UNKNOWN_DOCS_HTML",total:1,external:1},
 {name:"asset_query",body:`<script src="//yastatic.net/s3/psf/FAKE_DOC_39481.js?token=FAKE_QUERY_7863#FAKE_FRAGMENT_928"></script>`,page:"UNKNOWN_DOCS_HTML",total:1,external:1},
 {name:"unicode",body:`<html lang=ru><div id=root>Привет 世界 FAKE_BODY_SECRET</div></html>`,page:"GENERIC_DOCS_SHELL"},
 {name:"mixed_case",body:`<HTML><DIV ID="editor"></DIV><SCRIPT TYPE="APPLICATION/JSON">`+requiredJSON+`</SCRIPT></HTML>`,page:"EDITOR_LIKE_INLINE_BOOTSTRAP",total:1,inline:1,jsons:1,required:true,ttl:true},
 {name:"legal_quotes",body:`<div id='editor'></div><script type=application/json id='other'>`+requiredJSON+`</script>`,page:"EDITOR_LIKE_INLINE_BOOTSTRAP",total:1,inline:1,jsons:1,required:true,ttl:true},
 {name:"attribute_order",body:`<script class=x type='application/json' id=other>`+requiredJSON+`</script><div class=a id=editor></div>`,page:"EDITOR_LIKE_INLINE_BOOTSTRAP",total:1,inline:1,jsons:1,required:true,ttl:true},
 {name:"tokenizable_malformed",body:`<html><main id=app><p><b>FAKE_BODY_SECRET</main>`,page:"GENERIC_DOCS_SHELL"},
 {name:"json_depth",body:`<script type=application/json>`+strings.Repeat(`[`,StructureDepthLimit+2)+`0`+strings.Repeat(`]`,StructureDepthLimit+2)+`</script>`,page:"UNKNOWN_DOCS_HTML",limit:"DEPTH",total:1,inline:1,jsons:1},
 {name:"html_depth",body:strings.Repeat(`<div>`,StructureHTMLDepthLimit+1),page:"UNKNOWN_DOCS_HTML",limit:"DEPTH"},
 {name:"large_public_store",body:`<div id=root></div><script id=initial-store type=application/json>`+strings.TrimSuffix(landingJSON,"}")+`,"unused":"`+strings.Repeat("x",525000)+`"}</script>`,page:"PUBLIC_LANDING_INITIAL_STORE",total:1,inline:1,jsons:1},
 {name:"initial_required",body:`<script id=initial-store type=application/json>`+requiredJSON+`</script>`,page:"EDITOR_LIKE_INLINE_BOOTSTRAP",total:1,inline:1,jsons:1,required:true,ttl:true},
 {name:"ttl_absent",body:`<script type=application/json>{"officeActionData":{"action_url":"x","access_token":12}}</script>`,page:"EDITOR_LIKE_INLINE_BOOTSTRAP",total:1,inline:1,jsons:1,required:true},
 {name:"fields_missing",body:`<script type=application/json>{"officeActionData":{"action_url":"x"}}</script>`,page:"UNKNOWN_DOCS_HTML",total:1,inline:1,jsons:1},
 {name:"fields_empty",body:`<script type=application/json>{"officeActionData":{"action_url":"","access_token":false}}</script>`,page:"UNKNOWN_DOCS_HTML",total:1,inline:1,jsons:1},
 {name:"duplicate_json_keys",body:`<script type=application/json>{"officeActionData":{},"officeActionData":{}}</script>`,page:"UNKNOWN_DOCS_HTML",total:1,inline:1,jsons:1},
 {name:"json_trailing",body:`<script type=application/json>`+requiredJSON+` {}</script>`,page:"UNKNOWN_DOCS_HTML",total:1,inline:1,jsons:1},
 {name:"duplicate_initial_store",body:`<script id=initial-store type=application/json>{}</script><script id=initial-store type=application/json>{}</script>`,page:"AMBIGUOUS_STRUCTURE",total:2,inline:2,jsons:2},
 {name:"fake_markup_in_script",body:`<script>var x='<main id="unsupported-browser"><a data-action="update-browser"></a></main>';</script><!-- <script type=application/json>`+requiredJSON+`</script> -->`,page:"UNKNOWN_DOCS_HTML",total:1,inline:1},
 {name:"browser_id_only",body:`<main id=unsupported-browser></main>`,page:"UNKNOWN_DOCS_HTML"},
 {name:"browser_unrelated_anchor",body:`<main id=unsupported-browser></main><a data-action=update-browser></a>`,page:"UNKNOWN_DOCS_HTML"},
 {name:"text_only_browser_notice",body:`<html><title>Unsupported browser</title><p>Эта версия браузера устарела</p></html>`,page:"UNKNOWN_DOCS_HTML"},
 {name:"conflict_browser",body:browserFixture+challengeFixture,page:"AMBIGUOUS_STRUCTURE"},
}}

func structureResponse(c structureCase)*BootstrapResponse{return fixtureResponse(bootstrapCase{body:c.body,status:200})}
func TestSchema6StructureFixtures(t *testing.T) {
 for _,c:=range structureCases(){t.Run(c.name,func(t *testing.T){
  e:=structureResponse(c)
  if e.PageStructureClass!=c.page{t.Fatalf("page %s want %s",e.PageStructureClass,c.page)}
  limit:=c.limit;if limit==""{limit="NONE"}
  if e.StructureScanLimitReason!=limit||e.StructureScanComplete!=(limit=="NONE"){t.Fatalf("limit %s complete %v",e.StructureScanLimitReason,e.StructureScanComplete)}
  if e.ScriptTotalCount!=c.total||e.ScriptInlineCount!=c.inline||e.ScriptExternalCount!=c.external||e.ScriptJSONCount!=c.jsons||e.ScriptModuleCount!=c.modules{t.Fatal("script counts")}
  if e.RequiredAuthFieldShapePresent!=c.required||e.OptionalTTLFieldPresent!=c.ttl{t.Fatal("required auth shape")}
  if (e.PageStructureClass=="AMBIGUOUS_STRUCTURE")!=e.StructuralClassificationConflict{t.Fatal("conflict hidden")}
  if c.name=="no_scripts"&&(e.FormCount!=1||e.IframeCount!=1||e.NoscriptCount!=1||e.StylesheetCount!=1){t.Fatal("element counts")}
  assertStructurePrivacy(t,e)
 })}
}
func assertStructurePrivacy(t *testing.T,e *BootstrapResponse){t.Helper();b,err:=json.Marshal(e);if err!=nil{t.Fatal("marshal")}
 for _,secret:=range []string{"FAKE_","SECRET_SENTINEL","https:","http:","yastatic.net","yandex.ru","private.invalid","private-host.invalid","officeActionData","Unsupported browser","Эта версия","Привет","世界"}{if strings.Contains(string(b),secret){t.Fatal("structure leak")}}
}
func TestSchema6PrivacyAttackFixtures(t *testing.T){
 attacks:=[]string{"FAKE_DOC_39481","FAKE_OAUTH_927481","FAKE_ACCESS_99817","FAKE_QUERY_7863","FAKE_COOKIE_SESSION_291","FAKE_AUTHORIZATION_BEARER_839","fake-mail-497@private.invalid","private-host.invalid","FAKE_"+strings.Repeat("N",200),"FAKE_%2F%3F%26%3D_SECRET","FAKE_BODY_SECRET","FAKE_SCRIPT_SECRET","FAKE_JSON_KEY_SECRET"}
 for _,secret:=range attacks{t.Run(secret[:minIntForTest(12,len(secret))],func(t *testing.T){
  quoted,_:=json.Marshal(secret)
  body:=`<html><p>`+secret+`</p><script>`+secret+`</script><script type=application/json>{`+string(quoted)+`:`+string(quoted)+`}</script><script src="https://private.invalid/`+secret+`?token=FAKE_QUERY_7863#FAKE_FRAGMENT_928"></script></html>`
  e:=fixtureResponse(bootstrapCase{body:body,route:"https://docs.yandex.ru/edit/FAKE_DOC_39481?cookie=FAKE_COOKIE_SESSION_291",status:200})
  assertStructurePrivacy(t,e);b,_:=json.Marshal(e);if strings.Contains(string(b),secret){t.Fatal("attack leaked")}
 })}
}
func minIntForTest(a,b int)int{if a<b{return a};return b}
func TestSchema6PathAndResourceClasses(t *testing.T){
 paths:=map[string]string{"/":"DOCS_ROOT","/edit/FAKE_DOC_39481":"DOCS_EDITOR_LIKE","/viewer/FAKE_DOC_39481":"DOCS_VIEWER_LIKE","/i/FAKE_DOC_39481":"DOCS_PUBLIC_SHARE_LIKE","/document/error/FAKE_DOC_39481":"DOCS_ERROR_LIKE","/unrecognized/FAKE_DOC_39481":"DOCS_UNKNOWN_PATH_CLASS"}
 for p,w:=range paths{if finalPathClass("https://docs.yandex.ru"+p+"?token=FAKE_QUERY_7863")!=w{t.Fatal("path class")}}
 for _,raw:=range []string{"https://docs.yandex.ru.evil.invalid/edit/secret","https://secret@docs.yandex.ru/edit/secret","javascript:secret","//docs.yandex.ru/edit/secret"}{if finalPathClass(raw)!="UNKNOWN_PATH_CLASS"{t.Fatal("unsafe path")}}
 var f StaticResourceFamilies
 for _,raw:=range []string{"https://yastatic.net/s3/psf/x?q=secret","https://yastatic.net/s3/docs/x","//yastatic.net/x","https://static.yandex.net/x","https://yastatic.net.evil.invalid/x","/relative/x","data:text/javascript,secret"}{f.observe(raw)}
 if f.PSF!=1||f.Docs!=1||f.Generic!=1||f.OtherYandex!=1||f.ThirdParty!=1||f.Relative!=1||f.Unknown!=1{t.Fatal("family enum")}
}
func TestExportStructureFixtures(t *testing.T){
 dir:=os.Getenv("ACK_STRUCTURE_FIXTURE_DIR");if dir==""{t.Skip("Linux CI export")};if err:=os.MkdirAll(dir,0700);err!=nil{t.Fatal(err)}
 for _,c:=range structureCases(){e:=structureResponse(c);assertStructurePrivacy(t,e);b,err:=json.Marshal(e);if err!=nil{t.Fatal("marshal")};if err=os.WriteFile(filepath.Join(dir,c.name+".json"),b,0600);err!=nil{t.Fatal(err)}}
}

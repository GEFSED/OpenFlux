package ackdiag

import (
 "bytes"
 "compress/gzip"
 "encoding/json"
 "errors"
 "io"
 "net/http"
 "os"
 "path/filepath"
 "regexp"
 "strings"
 "sync"
 "syscall"
 "testing"
)

var frozenPattern=regexp.MustCompile(`<script[^>]*id="client-config"[^>]*>(.*?)</script>`)
const docsFixture=`<html><script id="client-config">{"officeActionData":{},"secret":"SECRET_SENTINEL"}</script></html>`
const challengeFixture=`<html><form action="/checkcaptcha"><input name="rep" value="SECRET_SENTINEL"></form></html>`
const syntheticURL="https://docs.yandex.ru/document/SECRET_SENTINEL?token=SECRET_SENTINEL"

type bootstrapCase struct{name,body,route string;status int;err error;encoding string;uncompressed bool;result string;terminal string}
func bootstrapCases() []bootstrapCase {return []bootstrapCase{
 {name:"normal",body:docsFixture,status:200,result:"CLIENT_CONFIG_FOUND"},
 {name:"docs_no_config",body:"<html><main>SECRET_SENTINEL</main></html>",status:200,result:"CLIENT_CONFIG_NOT_FOUND_2XX"},
 {name:"changed_format",body:`<html><script id='client-config'>{}</script></html>`,status:200,result:"CLIENT_CONFIG_NOT_FOUND_2XX"},
 {name:"whitespace_json",body:`<script id="client-config"> { "officeActionData" : {} } </script>`,status:200,result:"CLIENT_CONFIG_FOUND"},
 {name:"multiline_json",body:"<script id=\"client-config\">{\n\"officeActionData\":{}\n}</script>",status:200,result:"CLIENT_CONFIG_NOT_FOUND_2XX"},
 {name:"redirect",body:"redirect SECRET_SENTINEL",status:302,result:"REDIRECT"},
 {name:"redirect_docs",body:docsFixture,status:200,result:"CLIENT_CONFIG_FOUND"},
 {name:"redirect_auth",body:`<html><form action="https://passport.yandex.ru/auth"><input type="password" value="SECRET_SENTINEL"></form></html>`,route:"https://passport.yandex.ru/auth?token=SECRET_SENTINEL",status:200,result:"CLIENT_CONFIG_NOT_FOUND_2XX"},
 {name:"redirect_challenge",body:challengeFixture,route:"https://yandex.ru/showcaptcha?token=SECRET_SENTINEL",status:200,result:"CLIENT_CONFIG_NOT_FOUND_2XX"},
 {name:"403",body:`<html><div role="alert" data-error-code="access_denied">SECRET_SENTINEL</div></html>`,status:403,result:"CLIENT_CONFIG_NOT_FOUND_NON2XX"},
 {name:"404",body:"<html>missing SECRET_SENTINEL</html>",status:404,result:"CLIENT_CONFIG_NOT_FOUND_NON2XX"},
 {name:"429",body:"<html>busy SECRET_SENTINEL</html>",status:429,result:"CLIENT_CONFIG_NOT_FOUND_NON2XX"},
 {name:"500",body:"<html>failed SECRET_SENTINEL</html>",status:500,result:"CLIENT_CONFIG_NOT_FOUND_NON2XX"},
 {name:"empty",status:200,result:"EMPTY_BODY"},
 {name:"partial_eof",body:"<html>SECRET_SENTINEL",status:200,err:io.ErrUnexpectedEOF,result:"BODY_READ_PARTIAL"},
 {name:"config_before_read_error",body:docsFixture,status:200,err:io.ErrUnexpectedEOF,result:"BODY_READ_PARTIAL"},
 {name:"truncated_config",body:`<script id="client-config">{"secret":"SECRET_SENTINEL`,status:200,err:io.ErrUnexpectedEOF,result:"BODY_READ_PARTIAL"},
 {name:"gzip_success",body:docsFixture,status:200,uncompressed:true,result:"CLIENT_CONFIG_FOUND"},
 {name:"decompression_failure",status:200,uncompressed:true,err:gzip.ErrHeader,result:"BODY_READ_FAILED"},
 {name:"network_error",err:errors.New("GET https://secret.example/SECRET_SENTINEL"),result:"NETWORK_ERROR",terminal:"NETWORK_ERROR"},
 {name:"redirect_limit",result:"REDIRECT_LIMIT",terminal:"REDIRECT_LIMIT"},
 {name:"unknown_html",body:"<html><h1>SECRET_SENTINEL</h1></html>",status:200,result:"CLIENT_CONFIG_NOT_FOUND_2XX"},
 {name:"unsupported_encoding",body:"SECRET_SENTINEL",status:200,encoding:"br",result:"UNSUPPORTED_ENCODING"},
 {name:"invalid_json",body:`<script id="client-config">{SECRET_SENTINEL}</script>`,status:200,result:"CLIENT_CONFIG_FOUND"},
 {name:"non2xx_with_config",body:docsFixture,status:500,result:"CLIENT_CONFIG_FOUND"},
}}
func fixtureResponse(c bootstrapCase)*BootstrapResponse{
 raw:=c.route;if raw==""{raw=syntheticURL}
 if c.terminal!=""{e:=&BootstrapResponse{Schema:LogSchema,Event:"bootstrap_response",Ordinal:1,HTTPStatusClass:"OTHER",ContentTypeClass:"MISSING",ContentEncodingClass:"IDENTITY",BodyReadErrorClass:readErrorClass(c.err),FinalRouteClass:routeClass(raw,false),ResponseResult:c.terminal,ClientConfigParseResult:"NOT_ATTEMPTED"};if c.terminal=="REDIRECT_LIMIT"{e.Ordinal=11};return e}
 resp:=&http.Response{StatusCode:c.status,Header:make(http.Header),Uncompressed:c.uncompressed};resp.Header.Set("Content-Type","text/html; charset=utf-8");resp.Header.Set("Content-Encoding",c.encoding)
 e:=newBootstrapResponse(raw,resp,[]byte(c.body),c.err,frozenPattern);e.Ordinal=1
 if c.status<300||c.status>=400 {
  match:=frozenPattern.FindSubmatch([]byte(c.body));BootstrapSearch(e,len(match)>=2)
  if len(match)>=2{var cfg map[string]interface{};d:=json.NewDecoder(bytes.NewReader(match[1]));d.UseNumber();BootstrapParsed(e,d.Decode(&cfg))}
 }
 return e
}
func TestBootstrapOfflineFixtures(t *testing.T){
 for _,c:=range bootstrapCases(){t.Run(c.name,func(t *testing.T){
  e:=fixtureResponse(c);if e.ResponseResult!=c.result{t.Fatalf("result %s want %s",e.ResponseResult,c.result)}
  b,err:=json.Marshal(e);if err!=nil{t.Fatal("marshal")}
  if strings.Contains(string(b),"SECRET_SENTINEL")||strings.Contains(string(b),"https://")||strings.Contains(string(b),"officeActionData"){t.Fatal("sensitive output")}
  if c.terminal==""&&(e.BodyBytesRead!=uint64(len(c.body))||e.BodyReadComplete!=(c.err==nil)){t.Fatal("body accounting")}
  if c.name=="changed_format"||c.name=="multiline_json"{if !e.HasExpectedDocsBootstrapStructure||e.ClientConfigMatched{t.Fatal("production regexp changed")}}
  if c.name=="config_before_read_error"&&(!e.ClientConfigMatched||e.ClientConfigParseResult!="OK"||e.BodyReadComplete){t.Fatal("partial body acceptance changed")}
  if c.name=="redirect_challenge"&&(!e.HasKnownChallengeStructure||e.FinalRouteClass!="YANDEX_CAPTCHA_OR_CHALLENGE"){t.Fatal("challenge structure")}
  if c.name=="redirect_auth"&&(!e.HasKnownAuthStructure||e.FinalRouteClass!="YANDEX_AUTH"){t.Fatal("auth structure")}
  if c.name=="403"&&!e.HasKnownPermissionErrorStructure{t.Fatal("permission structure")}
  if c.name=="invalid_json"&&e.ClientConfigParseResult!="ERROR"{t.Fatal("JSON parse evidence")}
 })}
}
func TestBootstrapChallengeRequiresBodyStructure(t *testing.T){
 for _,raw:=range []string{"https://yandex.ru/showcaptcha?captcha=SECRET_SENTINEL",syntheticURL}{
  e:=fixtureResponse(bootstrapCase{body:"<html>captcha SECRET_SENTINEL</html>",route:raw,status:429})
  if e.HasKnownChallengeStructure||e.FinalRouteClass=="YANDEX_CAPTCHA_OR_CHALLENGE"{t.Fatal("inferred challenge")}
 }
 // Known marker inside script text or comments is not a DOM form.
 body:=`<script>var x='<form action="/checkcaptcha"><input name="rep">';</script><!-- <form action="/checkcaptcha"><input name="rep"> -->`
 e:=fixtureResponse(bootstrapCase{body:body,status:200});if e.HasKnownChallengeStructure{t.Fatal("text mistaken for structure")}
}
func TestBootstrapBoundsAndErrorClasses(t *testing.T){
 e:=fixtureResponse(bootstrapCase{body:strings.Repeat(docsFixture,80),status:200})
 if e.ClientConfigMatchCount!=64||!e.ClientConfigMatchCountCapped{t.Fatal("match allocation bound")}
 e=fixtureResponse(bootstrapCase{body:strings.Repeat("x",1024*1024+1),status:200});if e.StructureScanComplete{t.Fatal("scan cap")}
 for _,c:=range []struct{err error;want string}{{nil,"NONE"},{io.ErrUnexpectedEOF,"UNEXPECTED_EOF"},{gzip.ErrChecksum,"DECOMPRESSION_ERROR"},{syscall.ECONNRESET,"CONNECTION_RESET"},{os.ErrDeadlineExceeded,"TIMEOUT"},{errors.New("SECRET_SENTINEL"),"OTHER_IO_ERROR"}}{if readErrorClass(c.err)!=c.want{t.Fatal("error enum")}}
}
func TestBootstrapEmitterAndFailureIntegration(t *testing.T){
 previous:=startupRecorder.Load();defer startupRecorder.Store(previous)
 var out bytes.Buffer;r:=NewStartupRecorder(&out,&out);startupRecorder.Store(r)
 r.Record(ProcessStart,"ok",NoFailure);r.Record(Authorization,"begin",NoFailure)
 e:=fixtureResponse(bootstrapCase{body:challengeFixture,status:200});EmitBootstrap(e)
 StartupTransportFailure(errors.New("auth: client-config not found in "+syntheticURL))
 lines:=splitStartup(out.String());last:=decodeStartup(t,lines[len(lines)-1]);if last.Failure!=AuthChallenge{t.Fatal("body evidence not used")}
 if strings.Contains(out.String(),"SECRET_SENTINEL"){t.Fatal("leak")}
 var fallback bytes.Buffer;startupRecorder.Store(NewStartupRecorder(failingWriter{},&fallback));EmitBootstrap(e)
 if decodeStartup(t,strings.TrimSpace(fallback.String())).Failure!=SchemaFailure{t.Fatal("sink failure")}
}
func TestBootstrapConcurrentOutputAndCap(t *testing.T){
 previous:=startupRecorder.Load();defer startupRecorder.Store(previous)
 var out bytes.Buffer;r:=NewStartupRecorder(&out,&out);startupRecorder.Store(r)
 var wg sync.WaitGroup
 for i:=0;i<20;i++{wg.Add(1);go func(){defer wg.Done();EmitBootstrap(fixtureResponse(bootstrapCase{body:"redirect",status:302}))}()};wg.Wait()
 if r.bootstrapOrdinal!=12||!r.failed{t.Fatal("event cap not explicit")}
 if strings.Count(out.String(),"[ACK-BOOTSTRAP]")!=11{t.Fatal("event count")}
}
func TestExportBootstrapFixtures(t *testing.T){
 dir:=os.Getenv("ACK_BOOTSTRAP_FIXTURE_DIR");if dir==""{t.Skip("Linux CI export")}
 if err:=os.MkdirAll(dir,0700);err!=nil{t.Fatal(err)}
 for _,c:=range bootstrapCases(){b,err:=json.Marshal(fixtureResponse(c));if err!=nil{t.Fatal(err)};if err=os.WriteFile(filepath.Join(dir,c.name+".json"),b,0600);err!=nil{t.Fatal(err)}}
}

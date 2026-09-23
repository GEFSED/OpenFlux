package yandex

import (
 "bytes"
 "compress/gzip"
 "encoding/json"
 "fmt"
 "io"
 "net/http"
 "net/http/httptest"
 "os"
 "strings"
 "sync/atomic"
 "testing"

 "universal-bypass-tool/internal/ackdiag"
)

// Exercise the actual frozen authorization constructor/path against localhost.
// No session factory, request, redirect or parser is replaced by a test helper.
func TestProductionBootstrapObservationSemantics(t *testing.T){
 config:=`<script id="client-config">{}</script>`
 cases:=[]struct{name,body string;status int;partial,gzip,badGzip,redirect bool;want string}{
  {name:"found",body:config,status:200,want:"officeActionData missing"},
  {name:"changed_format",body:`<script id='client-config'>{}</script>`,status:200,want:"client-config not found"},
  {name:"non2xx_found",body:config,status:403,want:"officeActionData missing"},
  {name:"partial_found",body:config,status:200,partial:true,want:"officeActionData missing"},
  {name:"partial_missing",body:"<html>SECRET_SENTINEL",status:200,partial:true,want:"client-config not found"},
  {name:"gzip",body:config,status:200,gzip:true,want:"officeActionData missing"},
  {name:"gzip_error",body:"SECRET_SENTINEL",status:200,badGzip:true,want:"client-config not found"},
  {name:"redirect",body:config,status:200,redirect:true,want:"officeActionData missing"},
 }
 for _,c:=range cases{t.Run(c.name,func(t *testing.T){
  var calls atomic.Int32
  srv:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
   calls.Add(1)
   if r.Method!="GET"||r.Header.Get("User-Agent")!=volgaUserAgent||r.Header.Get("Accept-Language")!="ru-RU,ru;q=0.9"{t.Error("request semantics")}
   if c.redirect&&r.URL.Path=="/start"{w.Header().Set("Location","/final?token=SECRET_SENTINEL");w.WriteHeader(302);return}
   body:=[]byte(c.body);w.Header().Set("Content-Type","text/html")
   if c.gzip{var b bytes.Buffer;z:=gzip.NewWriter(&b);_,_=z.Write(body);_=z.Close();body=b.Bytes()}
   if c.gzip||c.badGzip{w.Header().Set("Content-Encoding","gzip")}
   if c.partial{w.Header().Set("Content-Length",fmt.Sprint(len(body)+7))}
   w.WriteHeader(c.status);_,_=w.Write(body)
  }));defer srv.Close()
  old:=os.Stderr;rd,wr,err:=os.Pipe();if err!=nil{t.Fatal(err)}
  os.Stderr=wr;ackdiag.BeginStartup();ackdiag.StartupBegin(ackdiag.Authorization)
  _,authErr:=authorize(srv.URL+"/start?token=SECRET_SENTINEL")
  _=wr.Close();os.Stderr=old;data,_:=io.ReadAll(rd);_=rd.Close()
  if authErr==nil||!strings.HasPrefix(authErr.Error(),c.want){t.Fatal("production return behavior differs")}
  wantCalls:=int32(1);if c.redirect{wantCalls=2};if calls.Load()!=wantCalls{t.Fatal("unexpected retry/request")}
  if bytes.Contains(data,[]byte("SECRET_SENTINEL"))||bytes.Contains(data,[]byte(srv.URL)){t.Fatal("secret output")}
  var last ackdiag.BootstrapResponse;n:=0
  for _,line:=range strings.Split(string(data),"\n"){if strings.HasPrefix(line,"[ACK-BOOTSTRAP] "){if json.Unmarshal([]byte(strings.TrimPrefix(line,"[ACK-BOOTSTRAP] ")),&last)!=nil{t.Fatal("JSON")};n++}}
  if n!=int(wantCalls){t.Fatal("response records")}
  if c.partial&&(last.BodyReadComplete||last.BodyReadErrorClass!="UNEXPECTED_EOF"){t.Fatal("partial read lost")}
  if c.gzip&&last.ContentEncodingClass!="GZIP_AUTO_DECODED"{t.Fatal("transparent gzip not observed")}
  if c.badGzip&&last.BodyReadErrorClass!="DECOMPRESSION_ERROR"{t.Fatal("gzip failure not observed")}
  if c.status==403&&!last.ClientConfigSearchOnNon2XX{t.Fatal("non2xx search not observed")}
 })}
}

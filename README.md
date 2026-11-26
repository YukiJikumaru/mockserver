# mockserver

HTTP クライアントを試すためのモックサーバーです。

決まった応答を返すエンドポイントのほかに、**接続の失敗**を起こせます。たとえば次のような失敗です。

- 要求を受けたあと、応答せずに接続を切る
- 使い回された接続の 2 本目で切る
- TLS のハンドシェイクの途中で切る
- プロキシの CONNECT で失敗させる

リトライやエラー処理が、失敗の段階ごとにどう動くかを確かめるのに使えます。

## 必要なもの

- Go 1.27.4 以上
  - Go 1.21 以降の go コマンドなら、`GOTOOLCHAIN=auto`（既定）のときに必要な版を自動で取ってきます

## 起動

```sh
go run .                # http://localhost:3001
go run . -s -p 3443     # https://localhost:3443
make build              # dist/mockserver_linux と dist/mockserver_windows を作る
```

使い方の一覧は `/help` でも見られます。

```sh
curl http://localhost:3001/help
```

## オプション

| フラグ | 既定 | 内容 |
|---|---|---|
| `-p` | `3001` | 待ち受けるポート |
| `-s` | なし | HTTPS で待ち受ける（[HTTPS の証明書](#https-の証明書)） |
| `-rt` | `0` | 要求全体を読むときのタイムアウト（ミリ秒）。0 は無制限 |
| `-rht` | `0` | 要求のヘッダーを読むときのタイムアウト（ミリ秒）。0 のときは `-rt` の値を使う |
| `-wt` | `0` | 応答を書くときのタイムアウト（ミリ秒）。0 は無制限 |
| `-it` | `0` | keep-alive で次の要求を待つ時間（ミリ秒）。0 のときは `-rt` の値を使う |
| `-gz` | なし | クライアントが `Accept-Encoding: gzip` を送ったときに、応答を gzip で圧縮する |
| `-tlsfail-port` | `0` | TLS のハンドシェイク中に切るポート。0 で無効（[TLS のハンドシェイク中に切る](#tls-のハンドシェイク中に切る)） |
| `-tlsfail-mode` | `close_notify` | `close_notify`・`reset`・`eof` のどれか |
| `-proxy-port` | `0` | 偽プロキシのポート。0 で無効（[偽プロキシ](#偽プロキシ)） |
| `-proxy-mode` | `tunnel` | `tunnel`・`503`・`close_notify`・`reset`・`eof` のどれか |

## 普通の応答を返すエンドポイント

| パス | メソッド | 応答 |
|---|---|---|
| `/`・`/dump` | すべて | 受け取った要求（要求行・Host・ヘッダー・本文）をテキストで返す |
| `/help` | GET | 使い方の一覧 |
| `/nothing` | GET | 本文が空の 200 |
| `/statuscode/:code` | GET | 指定したステータスコードを、本文なしで返す |
| `/sleep?msec=N` | GET | N ミリ秒待ってから 200 を返す |
| `/301redirect?url=URL`・`/302redirect?url=URL` | GET | `URL` へリダイレクトする |
| `/json/valid`・`/json/invalid` | GET | 正しい JSON、または途中で切れた JSON |
| `/streaming` | GET | 1 秒ごとに 1 行ずつ、5 行返す |
| `/streaming/infinite` | GET | 1 秒ごとに `HELLO` を返し続ける |
| `/invalidlength/short` | GET | `Content-Length: 100` と宣言しておき、5 バイト（`Hello`）だけ送って閉じる |
| `/invalidlength/long` | GET | `Content-Length: 1` と宣言するが、本文は送らずに閉じる（Go の net/http は宣言より長い書き込みを丸ごと拒むため） |
| `/nabeatsu/:num` | GET | 3 の倍数なら 200 でその数を、そうでなければ 500 で `AHO` を返す |

## 接続の失敗を起こす

### 応答せずに切る

| パス | メソッド | 動き |
|---|---|---|
| `/close/eof` | すべて | 要求を本文まで読んでから、応答せずに FIN で閉じる |
| `/close/reset` | すべて | 要求を本文まで読んでから、応答せずに RST で切る |
| `/keepalive/eof?nth=N` | すべて | 同じ接続の N 本目（既定は 2）の要求を読んだところで、FIN で閉じる。それより前の要求には 200 を返す |
| `/keepalive/reset?nth=N` | すべて | 同じく RST で切る |

- 要求は本文まで読み切ってから閉じます。読み残しがあると、OS は FIN ではなく RST を送ってしまうためです。
- `-s` で起動していても TLS の close_notify は送らず、下の TCP をそのまま閉じます。
- `/keepalive/*` は、パスを問わず、その接続で受けた要求の本数を数えます。
  - 「使い回した接続を、相手が閉じていた」ときのクライアントの動きを確かめるのに使えます。たとえば Go の net/http は、この場合 GET は新しい接続で送り直し、POST は送り直しません（`failure_test.go` で確かめています）。

### TLS のハンドシェイク中に切る

`-tlsfail-port` で指定したポートは、HTTP を話しません。ClientHello のレコードを 1 つ受け取ったところで、`-tlsfail-mode` のとおりに接続を切ります。

```sh
go run . -tlsfail-port 4443 -tlsfail-mode close_notify
curl -k https://localhost:4443/
```

| `-tlsfail-mode` | サーバーの動き | Ruby（OpenSSL 1.1.1f）に出るエラー | Ruby（OpenSSL 3.0.8）に出るエラー |
|---|---|---|---|
| `close_notify` | TLS の close_notify のアラートだけを返して閉じる | `OpenSSL::SSL::SSLError: SSL_connect returned=6 errno=0 peeraddr=... state=SSLv3/TLS write client hello` | 1.1.1f と同じ |
| `reset` | RST で切る | `Errno::ECONNRESET: Connection reset by peer - SSL_connect` | 1.1.1f と同じ |
| `eof` | 何も返さずに FIN で閉じる | `OpenSSL::SSL::SSLError: SSL_connect SYSCALL returned=5 errno=0 peeraddr=... state=SSLv3/TLS write client hello` | `OpenSSL::SSL::SSLError: SSL_connect returned=1 errno=0 peeraddr=(null) state=error: unexpected eof while reading` |

`state=SSLv3/TLS write client hello` は、OpenSSL のハンドシェイクの段階の名前です。SSLv3 で接続しているわけではありません。

### 偽プロキシ

`-proxy-port` で指定したポートは、CONNECT だけを受け付ける HTTP プロキシになります。CONNECT 以外のメソッドには 405 を返します。

| `-proxy-mode` | 動き |
|---|---|
| `tunnel` | 宛先につないで中継する。宛先につながらなければ、CONNECT に 503 を返す |
| `503` | CONNECT に 503 を返す |
| `close_notify`・`reset`・`eof` | CONNECT に 200 を返したあと、ClientHello を受けたところで、[TLS のハンドシェイク中に切る](#tls-のハンドシェイク中に切る) と同じ方法で切る |

プロキシの中で失敗すると、OpenSSL のエラーの `peeraddr` には、宛先ではなくプロキシの番地が出ます。

```sh
# HTTPS の本体（3443）と、close_notify で切る偽プロキシ（8080）
go run . -s -p 3443 -proxy-port 8080 -proxy-mode close_notify
```

```ruby
require 'net/http'

http = Net::HTTP.new('localhost', 3443, '127.0.0.1', 8080)
http.use_ssl = true
http.get('/help')
# => OpenSSL::SSL::SSLError: SSL_connect returned=6 errno=0 peeraddr=127.0.0.1:8080 state=SSLv3/TLS write client hello
```

`-proxy-mode 503` のときは、Ruby の Net::HTTP は `Net::HTTPFatalError: 503 "Service Unavailable"` を投げます。

`tunnel` には制限が 1 つあります。中継中に宛先が RST で切っても、偽プロキシはクライアント側を FIN で閉じます。そのため、クライアントには ECONNRESET ではなく EOF に見えます。

## HTTPS の証明書

`-s` では、`main.go` に埋め込んだ自己署名の証明書を使います。

- 名前: CN は `localhost`、SAN は `localhost` と `127.0.0.1`
- 期限: 2036-11-23
- HTTP/2 は使いません（HTTP/1.1 だけ）

証明書の検証ありでつなぐには、この証明書を信頼する CA として渡してください。動いているサーバーから取り出せます。

```sh
openssl s_client -connect localhost:3443 -servername localhost </dev/null 2>/dev/null | openssl x509 > localhost.crt

curl --cacert localhost.crt https://localhost:3443/help
```

```ruby
http = Net::HTTP.new('localhost', 3443)
http.use_ssl = true
http.ca_file = 'localhost.crt'
http.get('/help')
```

証明書を作り直す手順は、`main.go` の `localhostCert` の上のコメントにあります。`keyUsage` には `keyCertSign` を入れてください。入っていないと、OpenSSL を使うクライアント（Ruby など）は、この証明書を `ca_file` に渡しても検証に失敗します。

## テスト

```sh
go test ./...
```

接続の失敗を起こす機能のテストは、`failure_test.go` にあります。

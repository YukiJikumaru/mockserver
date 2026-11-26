# mockserver

HTTP クライアントを試すためのモックサーバーです。

決まった応答を返すエンドポイントに加えて、**接続の失敗**を起こせます。

- 要求を受けたあと、応答せずに接続を切る
- 同じ接続の N 本目の要求で切る
- TLS のハンドシェイクの途中で切る
- プロキシの CONNECT で失敗させる
- 同じ要求を繰り返し送ると、失敗してから成功する

クライアントのタイムアウト・リトライ・エラー処理が、それぞれの失敗でどう動くかを確かめるのに使えます。

## 必要なもの

- Go 1.27.4 以上
  - Go 1.21 以降の go コマンドなら、`GOTOOLCHAIN=auto`（既定）のときに必要な版を自動で取ってきます

## 起動

```sh
go run .                # http://localhost:3001
go run . -s -p 3443     # https://localhost:3443
make build              # dist/mockserver_linux と dist/mockserver_windows を作る
```

使い方の一覧は `/help` で見られます。

```sh
ruby -rnet/http -e 'puts Net::HTTP.get(URI("http://localhost:3001/help"))'
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

## 決まった応答を返す

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
| `/invalidlength/long` | GET | `Content-Length: 1` と宣言するが、本文は送らずに閉じる |
| `/nabeatsu/:num` | GET | 3 の倍数なら 200 でその数を、そうでなければ 500 で `AHO` を返す |

```ruby
require 'net/http'

# 受け取った要求をそのまま返す
res = Net::HTTP.post(URI('http://localhost:3001/dump'), '{"name":"hello"}', 'Content-Type' => 'application/json')
puts res.body

# 指定したステータスコードを返す
res = Net::HTTP.get_response(URI('http://localhost:3001/statuscode/503'))
puts res.code # => 503

# 応答まで 3 秒かかるので、read_timeout が 1 秒なら読み取りのタイムアウトになる
http = Net::HTTP.new('localhost', 3001)
http.read_timeout = 1
begin
  http.get('/sleep?msec=3000')
rescue => e
  puts "#{e.class}: #{e.message}"
end
```

## 接続の失敗を起こす

### 応答せずに切る

| パス | メソッド | 動き |
|---|---|---|
| `/close/eof` | すべて | 要求を本文まで読んでから、応答せずに FIN で閉じる |
| `/close/reset` | すべて | 要求を本文まで読んでから、応答せずに RST で切る |

- 要求は本文まで読み切ってから閉じます。読み残しがあると、OS は FIN ではなく RST を送ってしまうためです。
- `-s` で起動していても TLS の close_notify は送らず、下の TCP をそのまま閉じます。

```ruby
require 'net/http'

# 本文を読み終えてから、応答せずに RST で切られる
http = Net::HTTP.new('localhost', 3001)
begin
  http.post('/close/reset', 'x' * 1024)
rescue => e
  puts "#{e.class}: #{e.message}"
end
```

### 同じ接続の N 本目で切る（`/keepalive`）

使い回していた keep-alive の接続を相手が閉じていた、という状況を作ります。クライアントが、切られた要求を新しい接続で送り直すかどうかを確かめるのに使います。

| パス | メソッド | 動き |
|---|---|---|
| `/keepalive/eof?nth=N` | すべて | 同じ接続の N 本目の要求を読んだところで、応答せずに FIN で閉じる |
| `/keepalive/reset?nth=N` | すべて | 同じく、応答せずに RST で切る |

`nth` は 1 以上の整数で、省略すると 2 です。`nth=1` なら `/close/*` と同じ動きになります。

N 本目より前の要求には、200 で次の本文を返します。`n` はその接続で何本目の要求か、`N` は `nth` の値です。

```
request n on this connection (closes at N)
```

本数の数え方は次のとおりです。

- TCP の接続ごとに数えます。新しい接続では 1 から数え直します。
  - クライアントが新しい接続で送り直すと、その応答は `request 1 on this connection` になります。本文を見れば、新しい接続で届いたかが分かります。
  - クライアントが接続を使い回さない（要求のたびに新しい接続を作る）と、毎回 1 本目になるので切られません。
- パスを問わず数えます。`/nothing` に送った次に `/keepalive/eof` を送れば、2 本目として切られます。エラーになった要求（404 や 405）も数えます。
- メソッドを問わず数えます。POST も 1 本として数えます。

ほかの決まりは次のとおりです。

- 切る前に、要求を本文まで読み切ります。`-s` で起動していても TLS の close_notify は送りません（[応答せずに切る](#応答せずに切る) と同じ）。
- `eof`・`reset` 以外のパスや、`nth` が 1 以上の整数でないときは、400 を返します。
- 切ったときは、サーバーのログに次のような行が出ます。

  ```
  2026/11/26 11:22:06 close: eof GET /keepalive/eof?nth=2 from 127.0.0.1:50000
  ```

```ruby
require 'net/http'

# 同じ接続で 2 本送る。2 本目はサーバーが応答せずに閉じる
Net::HTTP.start('localhost', 3001) do |http|
  puts http.get('/keepalive/eof').body # => request 1 on this connection (closes at 2)
  begin
    # クライアントが新しい接続で送り直すと、その接続の 1 本目として 200 が返る
    puts http.get('/keepalive/eof').body
  rescue => e
    puts "#{e.class}: #{e.message}"
  end
end

# 3 本目で RST で切る。クライアントの送り直しを止めて、切られたことをそのまま見る
Net::HTTP.start('localhost', 3001) do |http|
  http.max_retries = 0
  3.times do |i|
    puts http.get('/keepalive/reset?nth=3').body
  rescue => e
    puts "#{i + 1} 本目: #{e.class}: #{e.message}"
  end
end
```

### TLS のハンドシェイク中に切る

`-tlsfail-port` で指定したポートは、HTTP を話しません。ClientHello のレコードを 1 つ受け取ったところで、`-tlsfail-mode` のとおりに接続を切ります。

| `-tlsfail-mode` | 動き |
|---|---|
| `close_notify` | TLS の close_notify のアラートだけを返して閉じる |
| `reset` | RST で切る |
| `eof` | 何も返さずに FIN で閉じる |

```sh
go run . -tlsfail-port 4443 -tlsfail-mode close_notify
```

```ruby
require 'net/http'

http = Net::HTTP.new('localhost', 4443)
http.use_ssl = true
begin
  http.start
rescue => e
  puts "#{e.class}: #{e.message}"
end
```

### 偽プロキシ

`-proxy-port` で指定したポートは、CONNECT だけを受け付ける HTTP プロキシになります。CONNECT 以外のメソッドには 405 を返します。

| `-proxy-mode` | 動き |
|---|---|
| `tunnel` | 宛先につないで中継する。宛先につながらなければ、CONNECT に 503 を返す |
| `503` | CONNECT に 503 を返す |
| `close_notify`・`reset`・`eof` | CONNECT に 200 を返したあと、ClientHello を受けたところで、[TLS のハンドシェイク中に切る](#tls-のハンドシェイク中に切る) と同じ方法で切る |

`tunnel` で中継している間に宛先が RST で切っても、偽プロキシはクライアント側を FIN で閉じます。RST はクライアントまで伝わりません。

```sh
# HTTPS の本体（3443）と、偽プロキシ（8080）
go run . -s -p 3443 -proxy-port 8080 -proxy-mode close_notify
```

```ruby
require 'net/http'

# 3・4 番目の引数がプロキシ。-proxy-mode tunnel なら /help の応答が返る
http = Net::HTTP.new('localhost', 3443, 'localhost', 8080)
http.use_ssl = true
http.ca_file = 'localhost.crt' # HTTPS の証明書を参照
begin
  puts http.get('/help').code
rescue => e
  puts "#{e.class}: #{e.message}"
end
```

## 要求ごとに応答を変える（`/sequence`）

「1 回目は切られ、2・3 回目は 503、4 回目で成功する」のように、同じ要求を繰り返し送ったときの応答を、1 回ごとに変えます。リトライが最後に成功するか、何回送ったか、どれだけ間を空けたかを確かめるのに使います。

| パス | メソッド | 動き |
|---|---|---|
| `/sequence/:key?steps=...` | すべて | 同じ `key` への要求に、`steps` を先頭から 1 つずつ使って応答する。使い切ったら最後の 1 つを繰り返す |
| `/sequence/:key/history` | GET | その `key` で受けた要求の一覧を JSON で返す |
| `/sequence/:key/reset` | すべて | その `key` の回数と一覧を消して、数え直す |

`steps` には、次の step をカンマで区切って並べます。例: `steps=reset,503*2,200`

| step | 応答 |
|---|---|
| `200`〜`599` | そのステータスコードを返す |
| `eof`・`reset` | 要求を本文まで読んでから、応答せずに切る（[応答せずに切る](#応答せずに切る) と同じ） |
| `sleep:ミリ秒` | 指定した時間だけ待ってから 200 を返す |
| `step*N` | 同じ step を N 回使う。例: `503*3` |

- 応答には、その `key` で何回目の要求かを `X-Sequence-Attempt` ヘッダーで付けます。`eof`・`reset` は応答しないので付きません。
- `key` ごとに数えます。テストごとに別の `key` を使えば、並べて走らせても混ざりません。
- どの step を使うかは、その回に受けた要求の `steps` で決まります。同じ URL を送り直す前提です。
- `steps` が正しくないときは 400 を返し、回数には数えません。
- 回数と一覧は、サーバーのメモリにだけ持ちます。再起動すると消えます。

`/sequence/:key/history` の各項目は次のとおりです。

| 項目 | 内容 |
|---|---|
| `attempt` | 何回目の要求か |
| `time` | 受けた時刻 |
| `since_prev_ms` | 前の要求からの間隔（ミリ秒）。1 回目は 0 |
| `method` | メソッド |
| `step` | その回に使った step |
| `headers` | 要求のヘッダー。同じ名前が複数あるときは `, ` でつなぐ |
| `body` | 要求の本文 |

```ruby
require 'net/http'
require 'json'

# 1 回目は RST、2・3 回目は 503、4 回目で成功する
path = '/sequence/order-1?steps=reset,503*2,200'

# 試したいリトライの例: 成功するまで、1 秒ずつ空けて最大 5 回送る
Net::HTTP.start('localhost', 3001) do |http|
  http.max_retries = 0 # クライアント自身の送り直しを止め、送った回数をこのループの回数に揃える
  5.times do |i|
    res = http.get(path)
    puts "#{i + 1} 回目: #{res.code}"
    break if res.is_a?(Net::HTTPSuccess)

    sleep 1
  rescue => e
    puts "#{i + 1} 回目: #{e.class}"
    sleep 1
  end
end

# サーバーが受けた回数と間隔を確かめる
history = JSON.parse(Net::HTTP.get(URI('http://localhost:3001/sequence/order-1/history')))
history['attempts'].each do |a|
  puts "#{a['attempt']} 回目: #{a['step']}（前の要求から #{a['since_prev_ms']} ms）"
end

# 次の試験のために数え直す
Net::HTTP.post(URI('http://localhost:3001/sequence/order-1/reset'), '')
```

## HTTPS の証明書

`-s` では、`main.go` に埋め込んだ自己署名の証明書を使います。

- 名前: CN は `localhost`、SAN は `localhost` と `127.0.0.1`
- 期限: 2036-11-23
- HTTP/2 は使いません（HTTP/1.1 だけ）

証明書の検証ありでつなぐには、この証明書を信頼する CA として渡してください。動いているサーバーから取り出せます。

```ruby
require 'net/http'

# 動いているサーバーから証明書を取り出して保存する
http = Net::HTTP.new('localhost', 3443)
http.use_ssl = true
http.verify_mode = OpenSSL::SSL::VERIFY_NONE
http.start { File.write('localhost.crt', http.peer_cert.to_pem) }

# 保存した証明書を信頼する CA として渡し、検証ありでつなぐ
http = Net::HTTP.new('localhost', 3443)
http.use_ssl = true
http.ca_file = 'localhost.crt'
puts http.get('/help').code # => 200
```

証明書を作り直す手順は、`main.go` の `localhostCert` の上のコメントにあります。`keyUsage` には `keyCertSign` を入れてください。入っていないと、この証明書を CA として渡しても検証に失敗するクライアントがあります。

## テスト

```sh
go test ./...
```

接続の失敗を起こす機能のテストは、`failure_test.go` にあります。

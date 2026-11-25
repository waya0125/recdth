# recdth

BUFFALO の DTH10 / DTH30 シリーズ（DT-H70/U2、DT-H45/U2 など）の USB チューナーを、`recpt1` / `recisdb` / `recfriio` / `recgvmvp` / `recdyna` と同じ引数で使うためのラッパーです。  
チューナーの制御そのものは、ユーザー空間ドライバー [i4M1k0SU/DTH10-series-driver](https://github.com/i4M1k0SU/DTH10-series-driver) の `dth` コマンドに任せ、recdth は次のことを受け持ちます。

- `recpt1` 互換の引数（`チャンネル 秒数 出力先`）と、チャンネル名の変換（`CS2` → `ND02` など）
- 設定ファイルのデバイスごとのプロファイルから、`dth tune`（直接）か `dth stream`（`dth serve` 経由）かを選ぶ
- device モードと raw モードの選択と、機種の制約に合わない指定を止める安全弁
- raw モードの TS を外部の B25 復号コマンドに通す（`--b25`）
- 録画時間・シグナル・出力先の切断で、子プロセスをプロセスグループごと確実に止める

> **状態**: 設計と骨組みの段階です。単体テスト（`go test ./...`）は通りますが、実機（DT-H70/U2・DT-H45/U2）と Mirakurun / mirakc での動作は未確認です。

## 設計方針

- **標準出力は TS 専用**: ログ・警告・エラーはすべて標準エラー出力に書きます。`--list` / `--status` の表示も標準エラー出力です。
- **dth の実在オプションだけを使う**: 組み立てる引数は `dth` v2.0.0（2026-11-23 公開）のソース（`crates/dth-cli`）で確認したものだけです。
  - `dth stream` には `-t` / `-o` / `--lnb` / `--app-keys` / `--firmware` がありません。録画時間と出力先は recdth が扱い、アンテナ電源・CoBDACom.dll・ファームウェアは `dth serve` 側で指定します。
  - `dth tune` には `-t` を渡し、dth 自身に止めさせます（recdth の計時は保険）。
- **安全側に倒す**: 機種の分からないデバイスや BS/CS 対応機では、`--mode auto` が raw を選びません。
- **録画サーバーに組み込める**: `dth serve` の常駐で 1 台の地デジと BS/CS を同時に配れます。他のクライアントが使用中のストリームは既定で奪いません（`dth stream --if-free`）。
- **Go の標準ライブラリ中心**: 外部依存は TOML の読み込み（`github.com/BurntSushi/toml`）だけです。

## 依存関係

| 種類 | もの | 使う場面 |
|---|---|---|
| 必須 | `dth`（[DTH10-series-driver](https://github.com/i4M1k0SU/DTH10-series-driver)、v2.0.0 で確認） | 常に |
| 必須（DTH10 系） | `CoBDACom.dll`（純正アプリ PCastTV3 に含まれる。351,232 バイト） | DTH10 系（DT-H70/U2 など）の device モード。DLL は実行せず認証情報の読み取りに使うため、Linux でも必要。BS/CS は device モードだけなので、BS/CS を見るなら必須 |
| 任意 | 外部 B25 復号コマンド（例: libaribb25 の `arib-b25-stream-test`）と `pcscd`・B-CAS カードリーダー | raw モード + `--b25` |
| 任意（DTH30 系） | `BufFsUSB.sys`（純正ドライバー） | DTH30 系で純正ファームウェアを使う場合だけ。無ければ dth 内蔵の互換ファームウェアで動く |

ファイルのハッシュなどは DTH10-series-driver の README を参照してください（2026-11-25 時点の情報）。

## device モードと raw モード

| | device モード | raw モード |
|---|---|---|
| 復号 | 機器内蔵の B-CAS で復号済み | 放送波そのまま（スクランブルあり） |
| 出力 | **1 選局につき 1 番組**（PSI + その番組の ES） | チャンネルの全番組 |
| 地デジ | ○ | ○（機種による。DT-F100/U2 などには無い） |
| BS/CS | ○ | **×（dth に BS/CS の raw モードは無い）** |
| 必要なもの | DTH10 系は `CoBDACom.dll` | 外部 B25 復号（`--b25`）か録画サーバー側のデコーダー |

### DT-H70/U2 の制約

- **device モードは 1 選局 1 番組だけ復号します**。同じチャンネルの別の番組を同時に録画・視聴することはできません。`--sid all` は最初の番組に置き換えるか、エラーにします（`sid_all` で選択）。
- **raw モードを一度使うと、電源を入れ直すまで BS/CS が受信できなくなります**。recdth は `satellite = true` のデバイスで raw を拒否します（承知の上で使うときだけ `allow_raw = true`）。
- アンテナ線に電源を出せません（`--lnb` は効きません）。

### Mirakurun / mirakc で使うときの注意

- device モードの出力には 1 番組しか入らないため、録画サーバーが「同じチャンネルの別番組」を同じチューナーのストリームで流用すると、その番組は映りません。全番組が必要なら DT-H45/U2 のように raw モード + `--b25` にしてください。
- device モードの出力に入る EIT（EPG）は機種によって違います。
  - DT-H45/U2（DTH30 系、dth の互換ファームウェア `dth30-fw`）: ヌルパケット以外の全 PID を流し、指定した番組の ES だけを復号して差し込む作りなので、EIT・SDT・NIT・TOT も入ります（`crates/dth30-fw/src/program.rs`）。
  - DT-H70/U2（DTH10 系）: ソースには「PSI と指定番組の PID」とあるだけで、EIT（特に PID `0x0012`）がどこまで通るかは機器側のファームウェア次第です。実機で数秒録って PID `0x0012` の有無を確認する予定です。

## ビルド

Go 1.24 以上が必要です。

```bash
CGO_ENABLED=0 go build -o bin/ ./cmd/...
go test ./...
```

## 準備

1. `dth` を `/usr/local/bin/dth` に置く（DTH10-series-driver の README の手順でビルド）。
2. udev ルールを入れる。

   ```bash
   sudo cp udev/60-buffalo-dth.rules /etc/udev/rules.d/
   sudo udevadm control --reload-rules
   sudo udevadm trigger --subsystem-match=usb --attr-match=idVendor=0411
   ```

3. `dth list` でシリアル番号（DTH30 系はユニット名）を確認する。
4. `config.example.toml` を `/etc/recdth/config.toml` にコピーして編集する。
5. `dth serve` を使うデバイスは、[systemd/dth-serve.service](systemd/dth-serve.service) を編集して常駐させる。

## 使い方

```bash
recdth [オプション] <チャンネル> <秒数|-> <出力先|->
recdth --list
recdth --status [--device 名前]
```

| 引数 | 説明 |
|---|---|
| `チャンネル` | 地デジ `13`〜`62`、BS `BS15_0` / `BS1_0`（`BS01_0` に正規化）、CS `CS2`〜`CS24`（`ND02`〜`ND24` に変換。`ND02` の直接指定も可）。`BS15_0:101` / `27:1024` のように `:SID` を付けると `--sid` と同じ意味（mirakc 向け） |
| `秒数` | 録画秒数。`-` または `0` で SIGINT / SIGTERM を受けるまで継続 |
| `出力先` | 出力ファイル。`-` で標準出力 |

| オプション | 説明 |
|---|---|
| `--device 名前` | 設定ファイルの論理デバイス名（`h70` など）、`dth serve` のソケットパス、またはシリアル／ユニット名。省略時は `default_device` |
| `--mode auto\|device\|raw` | 既定 `auto`。下の「モードの決め方」を参照 |
| `--sid SID\|hd\|all` | device モード: 数値は `--program SID`、`hd` と省略は `--program first`、`all` は警告して `first`（`sid_all = "error"` ならエラー）。raw モード: 受け取るだけ（将来の TS フィルター用。今は全番組を出力） |
| `--tsid HEX` | BS/CS の TSID を 16 進で明示（例: `4971` / `0x4971`）。BS は省略時にも既知の表（`dvbv5_channels_isdbs.conf` 準拠）から `--tsid` を自動付与する |
| `--b25` | raw モード: 設定の `b25_cmd` に通して復号する。device モード: 復号済みなので何もしない（`-v` で表示） |
| `--lnb 0\|11\|15` | BS/CS のアンテナ電源（dth の `0` / `110` / `150`）。`direct` 接続のときだけ効く |
| `--config パス` | 設定ファイル。既定は `$RECDTH_CONFIG` → `~/.config/recdth/config.toml` → `/etc/recdth/config.toml` |
| `--list` | `dth list` と設定済みデバイスを表示 |
| `--status` | `dth status`（`dth serve` の状態と C/N）を表示 |
| `-v` / `--verbose` | 詳しいログ。2 回重ねると dth にも `-v` を渡す |

### モードの決め方

1. BS/CS は常に device（`--mode raw` はエラー）。
2. `--mode device` / `--mode raw` を明示したらそれに従う。ただし raw は次の場合に拒否します（安全弁）。
   - `mode_policy = "device_only"` のデバイス
   - `satellite = true`（DT-H70/U2 など）で `allow_raw = true` が無いデバイス
3. `--mode auto`（既定）は `mode_policy` で決まります。

| `mode_policy` | 地デジ |
|---|---|
| `device_only` | 常に device |
| `raw_preferred` | raw（raw を使えないデバイスなら警告して device） |
| `auto` | device。`--b25` か `--sid all` のときだけ raw（設定に無い臨時のデバイスでは raw にしない） |

### 実行例

```bash
# DT-H70/U2（dth serve 経由）で BS15_0 の SID 101 を 60 秒録画
recdth --device h70 --sid 101 BS15_0 60 bs.ts

# DT-H45/U2 で地デジ 27ch を raw で受けて外部 B25 で復号し、30 分録画
recdth --device h45 --b25 27 1800 ch27.ts

# 標準出力へ無制限に出す（Ctrl-C で止まる）
recdth --device h70 CS2 - - | ffplay -

# デバイス一覧と dth serve の状態
recdth --list
recdth --status --device h70
```

### 終了コード

| コード | 意味 |
|---|---|
| `0` | 録画時間に達した・シグナルで止めた・出力先が閉じられた・dth が正常に終わった |
| `1` | dth や外部 B25 が異常終了した、TS が届かない（`first_byte_timeout`）など |
| `2` | 引数・設定ファイルの誤り、安全弁による拒否 |

## 設定ファイル

[config.example.toml](config.example.toml) に DT-H70/U2（`dth serve` 常駐・device 固定）と DT-H45/U2（直接・raw + 外部 B25）の 2 台構成の例があります。

| 項目 | 説明 |
|---|---|
| `dth_bin` | dth の実行ファイル（既定 `dth`） |
| `b25_cmd` | 外部 B25 復号コマンド（コマンドと引数の配列。標準入力→標準出力） |
| `default_device` | `--device` 省略時のデバイス |
| `sid_all` | device モードの `--sid all`: `fallback`（警告して first）/ `error` |
| `stop_grace` | SIGTERM から SIGKILL までの秒数（既定 5） |
| `first_byte_timeout` | 最初の TS が届くまでの上限秒数（既定 30、0 で無効） |
| `[devices.名前]` | デバイスごとのプロファイル（下表） |

| デバイスの項目 | 説明 |
|---|---|
| `model` | 表示用の型番 |
| `family` | `dth10` / `dth30`（dth30 は地デジ専用、`--app-keys` を渡さない） |
| `satellite` | BS/CS 対応機なら `true`（raw を拒否する） |
| `allow_raw` | `satellite = true` でも raw を許可する |
| `connection` | `serve`（`dth stream --socket`）/ `direct`（`dth tune`）。省略時は `socket` があれば serve |
| `mode_policy` | `device_only` / `raw_preferred` / `auto` |
| `socket` | `dth serve` のソケット |
| `serial` | USB シリアル番号（DTH30 系は `dth list` のユニット名。USB ポートを差し替えると変わる） |
| `app_keys` | `CoBDACom.dll`（direct のとき `dth tune --app-keys` に渡す） |
| `firmware` | `BufFsUSB.sys`（direct のとき `dth tune --firmware` に渡す） |
| `lnb` | アンテナ電源 `0` / `11` / `15`（direct のとき） |
| `tuner` | 2 チューナー機の地デジ側チューナー `0` / `1`（serve では `auto` も可） |
| `takeover` | `true` で他のクライアントが使用中のストリームを奪う（既定 `false` = `--if-free`） |
| `dth_bin` / `b25_cmd` | グローバル設定の上書き |

## Mirakurun / mirakc での設定例

### Mirakurun（`tuners.yml`）

DT-H70/U2 は `dth serve` が地デジと BS/CS を同時に配れるので、GR と BS/CS の 2 エントリにします。  
device モードは内蔵 B-CAS で復号済み、DT-H45/U2 は recdth が `--b25` で復号するので、どちらも `decoder` は不要です。

```yaml
- name: DT-H70-GR
  types:
    - GR
  command: /usr/local/bin/recdth --device h70 <channel> - -
  decoder: ~
  isDisabled: false

- name: DT-H70-BSCS
  types:
    - BS
    - CS
  command: /usr/local/bin/recdth --device h70 <channel> - -
  decoder: ~
  isDisabled: false

- name: DT-H45-GR
  types:
    - GR
  command: /usr/local/bin/recdth --device h45 --b25 <channel> - -
  decoder: ~
  isDisabled: false
```

`channels.yml` の BS は `BS15_0`、CS は `CS2` の表記のままで使えます。  
DT-H45/U2 の復号を Mirakurun のデコーダーに任せる場合は、`--b25` を外して `decoder` を指定してください（raw のままの TS が出ます）。

### mirakc（`config.yml`）

```yaml
tuners:
  - name: DT-H70-GR
    types: [GR]
    command: /usr/local/bin/recdth --device h70 {{{channel}}} - -
  - name: DT-H70-BSCS
    types: [BS, CS]
    command: /usr/local/bin/recdth --device h70 {{{channel}}} - -
  - name: DT-H45-GR
    types: [GR]
    command: /usr/local/bin/recdth --device h45 --b25 {{{channel}}} - -
```

mirakc の `tuners[].command` には `{{{sid}}}` がありません（`{{{sid}}}` が使えるのは `filters` 側だけです）。  
DT-H70/U2 の device モードで番組を指定したいときは、`channels[].channel` に `チャンネル:SID` の形で書くと、recdth が `:` の後ろを `--program` に渡します。

```yaml
channels:
  - name: BS1
    type: BS
    channel: 'BS15_0:101'
  - name: NHK総合
    type: GR
    channel: '27:1024'
```

### Docker で動かす録画サーバーから使う場合

- `dth serve` をホストで動かし、`/run/dth` をコンテナにマウントすると、コンテナには USB を渡さずに済みます。
- コンテナ内に `recdth` と `dth`（`dth stream` に使う。musl の静的バイナリ）と設定ファイルを置きます。
- ソケットは権限 0660 なので、コンテナ内のプロセスが `dth` グループの GID で動くようにしてください。

## 子プロセスの止め方

recdth は `dth`（と外部 B25 コマンド）を 1 つのプロセスグループで起動し、どの止まり方でも全員を回収してから終わります。

| きっかけ | 動き |
|---|---|
| 録画時間に達した / SIGINT・SIGTERM・SIGHUP | `dth` にだけ SIGTERM を送り、外部 B25 には EOF で残りを書き出させる |
| 出力先が閉じた（EPIPE） | プロセスグループ全体に SIGTERM |
| TS が届かない（`first_byte_timeout`） | プロセスグループ全体に SIGTERM、終了コード 1 |
| 上のあと `stop_grace` 秒たっても残っている | プロセスグループに SIGKILL |

Linux では `Pdeathsig` も設定しているので、recdth 自体が `kill -9` されても子に SIGTERM が届きます。

## ソースの構成

| パス | 内容 |
|---|---|
| `cmd/recdth` | エントリーポイント（シグナル、出力先、`--list` / `--status`） |
| `internal/args` | コマンドラインの解釈、モード・SID の決定、dth と外部 B25 の引数の組み立て |
| `internal/channel` | チャンネル名の正規化 |
| `internal/config` | 設定ファイルの読み込みと検証、`--device` の解決 |
| `internal/runner` | 子プロセスのパイプライン、計時、プロセスグループの停止 |
| `internal/logx` | stderr 専用のロガー |
| `systemd/` | `dth serve` 常駐用ユニットの雛形 |
| `udev/` | USB 機器の権限の udev ルール |

## 関連プロジェクト

- 同じ使い勝手の録画コマンド: [recdyna](https://github.com/waya0125/recdyna)、[recfriio](https://github.com/waya0125/recfriio)、[recgvmvp](https://github.com/waya0125/recgvmvp)
- 手元のチューナーの動作確認の一覧: [チューナー動作確認一覧](https://waya0125.github.io/tuners/)

## ライセンス

MIT License（[LICENSE](LICENSE)）。`dth` は DTH10-series-driver（MIT License）の成果物で、recdth には含みません。

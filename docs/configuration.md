# 設定・モード・録画サーバー連携

## 1. device モードと raw モード

| | device モード | raw モード |
|---|---|---|
| 復号 | 機器内蔵の B-CAS で復号済み | 放送波そのまま（スクランブルあり） |
| 出力 | **1 選局につき 1 番組**（PSI + その番組の ES） | チャンネルの全番組 |
| 地デジ | ○ | ○（機種による。DT-F100/U2 などには無い） |
| BS/CS | ○ | **×（`dth` に BS/CS の raw モードは無い）** |
| 必要なもの | DTH10 系は `CoBDACom.dll` | 外部 B25 復号（`--b25`）か録画サーバー側のデコーダー |

### モードの決め方

1. BS/CS は常に `device`（`--mode raw` はエラー）。
2. `--mode device` / `--mode raw` を明示したらそれに従う。ただし `raw` は次の場合に拒否します（安全弁）。
   - `mode_policy = "device_only"` のデバイス
   - `satellite = true`（DT-H70/U2 など）で `allow_raw = true` が無いデバイス
3. `--mode auto`（既定）は `mode_policy` で決まります。

| `mode_policy` | 地デジ |
|---|---|
| `device_only` | 常に `device` |
| `raw_preferred` | `raw`（raw を使えないデバイスなら警告して `device`） |
| `auto` | `device`。`--b25` か `--sid all` のときだけ `raw`（設定に無い臨時のデバイスでは `raw` にしない） |

### Mirakurun / mirakc で使うときの注意

- device モードの出力には 1 番組しか入らないため、録画サーバーが「同じチャンネルの別番組」を同じチューナーのストリームで流用すると、その番組は映りません。全番組が必要なら DT-H45/U2 のように raw モード + `--b25` にしてください。
- device モードの出力に入る EIT（EPG）は機種によって違います。
  - DT-H45/U2（DTH30 系、`dth` の互換ファームウェア `dth30-fw`）: ヌルパケット以外の全 PID を流し、指定した番組の ES だけを復号して差し込む作りなので、EIT・SDT・NIT・TOT も入ります（`crates/dth30-fw/src/program.rs`）。
  - DT-H70/U2（DTH10 系）: ソースには「PSI と指定番組の PID」とあり、EIT（特に PID `0x0012`）の通過範囲は機器側のファームウェア次第です。

## 2. 設定ファイル（`config.toml`）

[config.example.toml](../config.example.toml) に DT-H70/U2（`dth serve` 常駐・device 固定）と DT-H45/U2（直接・raw + 外部 B25）の 2 台構成の例があります。

### グローバル項目

| 項目 | 説明 |
|---|---|
| `dth_bin` | `dth` の実行ファイル（既定 `dth`） |
| `b25_cmd` | 外部 B25 復号コマンド（コマンドと引数の配列。標準入力→標準出力） |
| `default_device` | `--device` 省略時のデバイス |
| `sid_all` | device モードの `--sid all`: `fallback`（警告して `first`）/ `error` |
| `stop_grace` | SIGTERM から SIGKILL までの秒数（既定 5） |
| `first_byte_timeout` | 最初の TS が届くまでの上限秒数（既定 30、0 で無効） |
| `[devices.名前]` | デバイスごとのプロファイル（下表） |

### デバイス項目（`[devices.名前]`）

| 項目 | 説明 |
|---|---|
| `model` | 表示用の型番 |
| `family` | `dth10` / `dth30`（`dth30` は地デジ専用、`--app-keys` を渡さない） |
| `satellite` | BS/CS 対応機なら `true`（`raw` を既定で拒否） |
| `allow_raw` | `satellite = true` でも `raw` を許可する |
| `connection` | `serve`（`dth stream --socket`）/ `direct`（`dth tune`）。省略時は `socket` があれば `serve` |
| `mode_policy` | `device_only` / `raw_preferred` / `auto` |
| `socket` | `dth serve` のソケットパス |
| `serial` | USB シリアル番号（DTH30 系は `dth list` のユニット名。USB ポートを差し替えると変わる） |
| `app_keys` | `CoBDACom.dll`（`direct` のとき `dth tune --app-keys` に渡す） |
| `firmware` | `BufFsUSB.sys`（`direct` のとき `dth tune --firmware` に渡す） |
| `lnb` | アンテナ電源 `0` / `11` / `15`（`direct` のとき） |
| `tuner` | 2 チューナー機の地デジ側チューナー `0` / `1`（`serve` では `auto` も可） |
| `takeover` | `true` で他のクライアントが使用中のストリームを奪う（既定 `false` = `--if-free`） |
| `dth_bin` / `b25_cmd` | グローバル設定の上書き |

## 3. 録画サーバー・Docker 連携

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
- ソケットの権限は `0660` なので、コンテナ内のプロセスが `dth` グループの GID で動くようにしてください。

## 4. 終了コードと子プロセスの止め方

### 終了コード

| コード | 意味 |
|---|---|
| `0` | 録画時間に達した・シグナルで止めた・出力先が閉じられた・`dth` が正常に終わった |
| `1` | `dth` や外部 B25 が異常終了した、TS が届かない（`first_byte_timeout`）など |
| `2` | 引数・設定ファイルの誤り、安全弁による拒否 |

### 子プロセスの止め方

recdth は `dth`（と外部 B25 コマンド）を 1 つのプロセスグループで起動し、どの止まり方でも全員を回収してから終わります。

| きっかけ | 動き |
|---|---|
| 録画時間に達した / SIGINT・SIGTERM・SIGHUP | `dth` にだけ SIGTERM を送り、外部 B25 には EOF で残りを書き出させる |
| 出力先が閉じた（EPIPE） | プロセスグループ全体に SIGTERM |
| TS が届かない（`first_byte_timeout`） | プロセスグループ全体に SIGTERM、終了コード 1 |
| 上のあと `stop_grace` 秒たっても残っている | プロセスグループに SIGKILL |

Linux では `Pdeathsig` も設定しているため、recdth 自体が `kill -9` されても子に SIGTERM が届きます。

## 5. 設計方針とソースの構成

- **標準出力は TS 専用**: ログ・警告・エラー・`--list` / `--status` の表示はすべて標準エラー出力に書きます。
- **`dth` の実在オプションだけを使う**: 組み立てる引数は `dth` v2.0.0（2026-11-23 公開）のソース（`crates/dth-cli`）で確認したものだけです。`dth stream` には `-t` / `-o` / `--lnb` / `--app-keys` / `--firmware` が無いため、録画時間と出力先は recdth が扱い、残りは `dth serve` 側で指定します。`dth tune` には `-t` を渡し、`dth` 自身に止めさせます。
- **Go の標準ライブラリ中心**: 外部依存は TOML 読み込み（`github.com/BurntSushi/toml`）だけです。

| パス | 内容 |
|---|---|
| `cmd/recdth` | エントリーポイント（シグナル、出力先、`--list` / `--status`） |
| `internal/args` | コマンドライン解釈、モード・SID・TSID の決定、`dth` と外部 B25 の引数組み立て |
| `internal/channel` | チャンネル名の正規化と BS TSID 表 |
| `internal/config` | 設定ファイルの読み込みと検証、`--device` の解決 |
| `internal/runner` | 子プロセスのパイプライン、計時、プロセスグループの停止 |
| `internal/logx` | stderr 専用のロガー |
| `systemd/` | `dth serve` 常駐用ユニットの雛形 |
| `udev/` | USB 機器の権限の udev ルール |

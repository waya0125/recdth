# recdth

BUFFALO の DTH10 / DTH30 シリーズ（DT-H70/U2、DT-H45/U2 など）の USB チューナーを、`recpt1` / `recisdb` / `recfriio` / `recgvmvp` / `recdyna` と同じ引数で使うための録画コマンドです。  
チューナー制御はユーザー空間ドライバー [i4M1k0SU/DTH10-series-driver](https://github.com/i4M1k0SU/DTH10-series-driver) の `dth` コマンドに任せ、recdth はチャンネル変換・BS TSID の自動付与・モードの安全弁・外部 B25 連携・プロセス管理を受け持ちます。

## 対応機種・ドキュメント

| 機種 / テーマ | 内容 | ドキュメント |
|---|---|---|
| BUFFALO DT-H70/U2（DTH10 系） | 地デジ・BS・110 度 CS、制約、実機動作確認 | [docs/DT-H70-U2.md](docs/DT-H70-U2.md) |
| 設定・モード・録画サーバー連携 | `config.toml` 全項目、device/raw の決め方、Mirakurun/mirakc/Docker、設計と構成 | [docs/configuration.md](docs/configuration.md) |

## 関連プロジェクト

- ユーザー空間ドライバー: [i4M1k0SU/DTH10-series-driver](https://github.com/i4M1k0SU/DTH10-series-driver)（`dth` v2.0.0 で確認）
- 同じ使い勝手の録画コマンド: [recdyna](https://github.com/waya0125/recdyna)、[recfriio](https://github.com/waya0125/recfriio)、[recgvmvp](https://github.com/waya0125/recgvmvp)
- 手元のチューナーの動作確認の一覧: [チューナー動作確認一覧](https://waya0125.github.io/tuners/)

## 必要なもの

- `dth`（[DTH10-series-driver](https://github.com/i4M1k0SU/DTH10-series-driver)、v2.0.0 で確認）
- DTH10 系（DT-H70/U2 など）の device モード: 純正アプリ PCastTV3 に含まれる `CoBDACom.dll`（351,232 バイト。認証情報の読み取りに使うため Linux でも必要）
- raw モード + `--b25` を使う場合: 外部 B25 復号コマンド（例: `arib-b25-stream-test`）と `pcscd`・B-CAS カードリーダー
- DTH30 系で純正ファームウェアを使う場合のみ: `BufFsUSB.sys`（無ければ `dth` 内蔵の互換ファームウェアで動作）

ファイルのハッシュなどは DTH10-series-driver の README を参照してください（2026-11-25 時点の情報）。

## ビルド

Go 1.24 以上が必要です。

```bash
CGO_ENABLED=0 go build -o bin/ ./cmd/...
go test ./...
```

## 準備

1. `dth` を `/usr/local/bin/dth` に置きます（DTH10-series-driver の手順でビルド）。
2. udev ルールを入れます。

   ```bash
   sudo cp udev/60-buffalo-dth.rules /etc/udev/rules.d/
   sudo udevadm control --reload-rules
   sudo udevadm trigger --subsystem-match=usb --attr-match=idVendor=0411
   ```

3. `dth list` でシリアル番号（DTH30 系はユニット名）を確認します。
4. [config.example.toml](config.example.toml) を `/etc/recdth/config.toml` にコピーして編集します。
5. `dth serve` を使うデバイスは、[systemd/dth-serve.service](systemd/dth-serve.service) を編集して常駐させます。

## 使い方

```bash
recdth [オプション] <チャンネル> <秒数|-> <出力先|->
recdth --list
recdth --status [--device 名前]
```

| 引数 | 説明 |
|---|---|
| `チャンネル` | 地デジ `13`〜`62`、BS `BS15_0` / `BS1_0`（`BS01_0` に正規化）、CS `CS2`〜`CS24`（`ND02`〜`ND24` に変換。`ND02` の直接指定も可）。`BS15_0:101` のように `:SID` も指定可 |
| `秒数` | 録画秒数。`-` または `0` で SIGINT / SIGTERM まで継続 |
| `出力先` | 出力ファイル。`-` で標準出力（標準出力は TS 専用） |

| オプション | 説明 |
|---|---|
| `--device 名前` | 設定ファイルの論理デバイス名（`h70` など）、`dth serve` のソケットパス、またはシリアル／ユニット名。省略時は `default_device` |
| `--mode auto\|device\|raw` | 既定 `auto`。モードの決め方は [docs/configuration.md](docs/configuration.md) を参照 |
| `--sid SID\|hd\|all` | device モード: 数値は `--program SID`、`hd` と省略は `first`、`all` は警告して `first`（`sid_all = "error"` ならエラー） |
| `--tsid HEX` | BS/CS の TSID を 16 進で明示（例: `4971` / `0x4971`）。BS は省略時にも既知の表（`dvbv5_channels_isdbs.conf` 準拠）から `--tsid` を自動付与 |
| `--b25` | raw モード: 設定の `b25_cmd` で復号。device モード: 復号済みなのでそのまま出力 |
| `--lnb 0\|11\|15` | BS/CS のアンテナ電源（dth の `0` / `110` / `150`）。`direct` 接続時のみ有効 |
| `--config パス` | 設定ファイル（既定: `$RECDTH_CONFIG` → `~/.config/recdth/config.toml` → `/etc/recdth/config.toml`） |
| `--list` / `--status` | デバイス一覧（`dth list`）または `dth serve` の状態を表示 |
| `-v` / `--verbose` | 詳しいログ。2 回重ねると `dth` にも `-v` を渡す |

### 実行例

```bash
# DT-H70/U2（dth serve 経由）で BS15_0 の SID 101 を 60 秒録画
recdth --device h70 --sid 101 BS15_0 60 bs.ts

# DT-H45/U2 で地デジ 27ch を raw で受けて外部 B25 で復号し、30 分録画
recdth --device h45 --b25 27 1800 ch27.ts

# 標準出力へ無制限に出す（Ctrl-C で停止）
recdth --device h70 CS2 - - | ffplay -

# デバイス一覧と dth serve の状態
recdth --list
recdth --status --device h70
```

### Mirakurun（`tuners.yml`）の設定例

```yaml
- name: DT-H70-GR
  types: [GR]
  command: /usr/local/bin/recdth --device h70 <channel> - -
  decoder: ~
  isDisabled: false

- name: DT-H70-BSCS
  types: [BS, CS]
  command: /usr/local/bin/recdth --device h70 <channel> - -
  decoder: ~
  isDisabled: false

- name: DT-H45-GR
  types: [GR]
  command: /usr/local/bin/recdth --device h45 --b25 <channel> - -
  decoder: ~
  isDisabled: false
```

mirakc の設定例・Docker での運用・設定ファイル全項目は [docs/configuration.md](docs/configuration.md) を参照してください。

## ライセンス

MIT License（[LICENSE](LICENSE)）。`dth` は DTH10-series-driver（MIT License）の成果物であり、recdth には含みません。

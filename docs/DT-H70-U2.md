# BUFFALO DT-H70/U2

## 1. 概要

| 項目 | 内容 |
|---|---|
| USB ID | `0411:0199`（DTH10 系） |
| 放送波 | 地デジ・BS・110 度 CS（`dth serve` 経由で地デジと BS/CS を同時配信可） |
| モード | `device` モード（BS/CS は `device` モードのみ） |
| B-CAS | 本体のスロット（device モードは機器内蔵 B-CAS で復号済み） |
| 必要なファイル | `CoBDACom.dll`（純正アプリ PCastTV3 に含まれる。351,232 バイト） |
| アンテナ電源 | BS・CS アンテナへの電源供給なし（`--lnb` は効かない） |

## 2. 準備

1. [README の「準備」](../README.md#準備) に従い、`dth` と udev ルールを配置します。
2. `dth list` で DT-H70/U2 のシリアル番号（例: `04110199-...`）を確認します。
3. `/etc/recdth/config.toml` に `satellite = true`・`mode_policy = "device_only"` のプロファイルを書きます（[config.example.toml](../config.example.toml) を参照）。
4. `dth serve` に `--app-keys /path/to/CoBDACom.dll` を渡して常駐させます。

## 3. 使い方

```bash
# BS15_0（NHK BS）の SID 101 を 60 秒録画
recdth --device h70 --sid 101 BS15_0 60 bs.ts

# BS23_1（BSよしもと）を標準出力へ流す（既知の TSID 0x4971 を自動付与）
recdth --device h70 BS23_1 - - > bs23_1.ts

# TSID を 16 進で明示指定する
recdth --device h70 --tsid 0x4971 BS23_0 10 out.ts
```

mirakc で番組（SID）を指定する場合は、`channels[].channel` に `BS15_0:101` のように `:SID` を付けて書きます。

## 4. 動作確認の記録

2026-11-26 時点、Ubuntu 26.04.1 LTS（amd64）・`dth` v2.0.0（`dth serve`、device モード）で DT-H70/U2（`0411:0199`）の実機動作を確認しました。

| 指定 | 実測 TSID（PAT） | 実測 SID | 結果 |
|---|---|---|---|
| `BS23_1` | `0x4971` | `265`（BSよしもと） | 正常受信 |
| `BS23_2` | `0x4973` | `260` | 正常受信 |
| `BS13_2` | `0x46d2` | `231, 232, 531`（放送大学） | 正常受信 |
| `BS09_1` | `0x4092` | `222`（BS12） | 正常受信 |
| `BS15_0` | `0x40f1` | `101, 102, 700, 701, 707, 929`（NHK BS） | 正常受信 |
| `BS23_0 --tsid 0x4971` | `0x4971` | `265` | 正常受信 |

BS チャンネルは `recdth` が既知の TSID 表（`recisdb 1.3.2` 同梱 `dvbv5_channels_isdbs.conf` 準拠）から `dth --tsid <HEX>` を自動付与するため、`dth` 側でロック直後に TMCC の TSID 表が空で読まれた場合でも正しい TS を選局できます。

## 5. 注意事項

- **1 選局 1 番組の制限**: device モードは 1 選局につき 1 番組だけ復号します。同じチャンネルの別番組を同時に録画・視聴することはできません。`--sid all` は最初の番組（`first`）に置き換えるか、エラーにします（`sid_all` で選択）。
- **raw モードの制約**: DT-H70/U2 で raw モードを一度使うと、電源を入れ直すまで BS/CS が受信できなくなります。`recdth` は `satellite = true` のデバイスで raw を既定で拒否します（承知の上で使う場合のみ `allow_raw = true`）。
- **BS/CS アンテナ電源**: 本機はアンテナ線に電源を出しません。常時給電のある環境で使ってください。
- **EIT（EPG）**: DT-H70/U2（DTH10 系）の device モードでは「PSI と指定番組の PID」が出力されますが、EIT（PID `0x0012`）の通過範囲は機器側のファームウェア次第です。

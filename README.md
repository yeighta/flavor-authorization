# flavor-authorization

財務省「製造たばこ小売定価認可」公表PDFから水タバコ（シーシャ）情報を抽出し、メーカー別に閲覧できるWebサイト。

- データソース: <https://www.mof.go.jp/policy/tab_salt/topics/kouriteika.html>
- 収録範囲: 2018年4月以降に新規認可・価格改定された水タバコ製品
- 公開先: <https://flavor-authorization.y8a.jp>（Cloudflare Pages。`flavor-authorization.pages.dev` にも配信）

## アーキテクチャ

```
GitHub Actions (cron 1h)
  └─ cmd/collect-urls   財務省ページ + Wayback Machine から PDF URL 一覧を生成
  └─ cmd/scraper        各 PDF を OpenRouter のマルチモーダル LLM（既定 GPT-6 Luna、PDF をそのまま入力）で構造化 JSON 化（キャッシュ）
  └─ cmd/classify       メーカー名を OpenRouter web 検索付きで kiseru/shisha 分類
  └─ data/* を commit（PR 経由）
  └─ cmd/notify         新規商品・価格改定を Discord / X に通知
       ↓
  GitHub Actions (deploy)
  └─ Next.js static export → Cloudflare Pages
```

データは静的 JSON として `frontend/public/data/` に配置され、クライアントが `fetch` で読み込みます。

## ローカル開発

### 必要なもの
- Go 1.26+
- Node.js 20+
- pnpm
- poppler-utils（`pdftoppm`。画像入力モデルでページを PNG 化するため）
- OpenRouter API Key（<https://openrouter.ai/keys>）

### セットアップ
```bash
cp .env.example .env
# .env に OPENROUTER_API_KEY=... を記入（抽出モデルは EXTRACT_MODEL、分類・集約モデルは OPENROUTER_MODEL で変更可）

# Go 側依存解決
go mod download

# フロント依存解決
cd frontend && pnpm install
```

### 主要コマンド
```bash
# 未キャッシュの PDF をスクレイプ（フェッチ→LLM→data/extracted/*.json）
go run ./cmd/scraper

# 既存キャッシュを products.json にマージのみ（API 呼ばない）
go run ./cmd/scraper -skip-extract

# メーカー分類（locked のものは保護される）
go run ./cmd/classify
go run ./cmd/classify -refresh   # 全件再分類（locked は除く）

# メーカー名表記揺れの集約（エイリアスマップ生成）
go run ./cmd/normalize-manufacturers

# 抽出モデルの比較（既存キャッシュ＝Gemini 3 Flash の結果を基準に再現率・表記一致率・価格一致率・費用を出す）
go run ./cmd/eval-extract -models openai/gpt-6-luna,deepseek/deepseek-v4.1-flash

# 通知のプレビュー（送信しない）
go run ./cmd/notify -old-products old.json -dry-run

# フロント開発サーバ
cd frontend && pnpm dev
# http://localhost:3000
```

### 重複排除と価格改定

`products.json` の同一性キーは「区分・メーカー・名称・容量」です（`internal/model/types.go` の `Key()`）。名称・メーカーは大文字小文字・記号を無視し、容量は数値比較（`50g` = `50.0g`）します。製品の区分（箱/缶）は価格改定 PDF で省略されることが多いためキーに含めません。

後の PDF（主に価格改定）が既存キーに一致した場合は、価格・更新日・出典のみ更新し、名称や区分などの表示は最初の認可時のものを維持します。

### イベント駆動の更新

`watcher/` の Cloudflare Worker が 10 分ごとに財務省の一覧ページを確認し、`data/pdf-urls.json` にない PDF が載ったときだけ `update.yml` を起動します（実行中・直近 20 分以内に実行済みなら起動しません）。GitHub の定期実行は遅延が大きいため、6 時間ごとの保険としてだけ残しています。

Worker は GitHub App「flavor-authorization-watcher」（権限は Actions の読み書きのみ、このリポジトリにだけインストール）として GitHub Actions を起動します。実行のたびに 1 時間有効のトークンを発行するので、期限切れの管理は不要です。

- Secrets: `WATCHER_APP_ID`（App ID）と `WATCHER_APP_PRIVATE_KEY`（秘密鍵。PKCS#8 形式 = `openssl pkcs8 -topk8 -nocrypt -in key.pem` で変換したもの）
- 反映: Actions → "Deploy watcher"（`watcher/` を変更して main に push しても自動で走ります）
- 鍵を作り直すとき: GitHub → Settings → Developer settings → GitHub Apps → flavor-authorization-watcher → Private keys

### 手動オーバーライド

メーカー分類の手動修正は `data/manufacturers.json` を直接編集します。`"locked": true` を付けると `classify --refresh` で上書きされません。

```json
"ドーラ": {
  "manufacturer": "ドーラ",
  "type": "kiseru",
  "confidence": "high",
  "reason": "manual override: not shisha",
  "locked": true
}
```

## ホスティング手順

### 1. GitHub Secrets の登録
リポジトリ **Settings → Secrets and variables → Actions** で以下を登録：

| Secret | 用途 | 取得元 |
|---|---|---|
| `OPENROUTER_API_KEY` | スクレイパ＋分類 | <https://openrouter.ai/keys> |
| `DISCORD_WEBHOOK_URL` | Discord 通知（任意） | チャンネル設定 → 連携サービス → ウェブフック |
| `X_API_KEY` / `X_API_SECRET` | X 自動投稿（任意） | <https://developer.x.com> の App → Keys and tokens → Consumer Keys |
| `X_ACCESS_TOKEN` / `X_ACCESS_TOKEN_SECRET` | X 自動投稿（任意） | 同 Authentication Tokens（App の権限を **Read and write** にしてから発行） |

X への投稿は 1 回の更新につき 1 件（URL 付きのため X API で 1 件 $0.20）。新しく公表された PDF の分だけを告知するので、同じ内容が再投稿されることはありません。投稿のリンクはその公表分に絞り込んだ URL（`?date=2026-10-02`）で、1 ブランドだけの更新ならブランドも絞り込みます（`?brand=BALLI&date=2026-10-02`）。

X の 4 つのシークレットは、リポジトリ直下の `.env.x`（gitignore 済み）に値を書いて `scripts/set-x-secrets.sh` を実行すると登録できます。Secrets 登録後の動作確認は Actions → "Test X post" を手動実行すると、直近の公表を 1 件だけ投稿します。
| `CLOUDFLARE_API_TOKEN` | Pages デプロイ | Cloudflare ダッシュボード "My Profile" → "API Tokens" → "Create Token" → "Edit Cloudflare Workers" テンプレで作成 |
| `CLOUDFLARE_ACCOUNT_ID` | Pages デプロイ | Cloudflare ダッシュボード右サイドの "Account ID" |

### 2. Cloudflare Pages プロジェクト作成
- Cloudflare ダッシュボード → **Workers & Pages → Create → Pages → Direct Upload**
- プロジェクト名: **flavor-authorization**
- ビルドはワークフローが行うので、ダッシュボード上は最小設定で構いません

### 3. 初回デプロイ
- main へ push すると `.github/workflows/deploy.yml` が走ります
- Actions タブでログ確認、Secret 漏れなら失敗します
- 成功すると `https://flavor-authorization.y8a.jp`（独自ドメイン）と `https://flavor-authorization.pages.dev` で公開されます

### 4. 自動更新 CD
- `.github/workflows/update.yml` が毎時 17 分に発火
- 新しい PDF があれば取り込み → `data/*.json` を commit → deploy.yml 連鎖
- 手動トリガは Actions タブ → "Update tobacco DB" → "Run workflow"

## ライセンス・免責

本プロジェクトは個人プロジェクトであり、財務省・たばこメーカーとの公式関係はありません。データの正確性は保証しません。

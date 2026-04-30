# flavor-authorization

財務省「製造たばこ小売定価認可」公表PDFから水タバコ（シーシャ）情報を抽出し、メーカー別に閲覧できるWebサイト。

- データソース: <https://www.mof.go.jp/policy/tab_salt/topics/kouriteika.html>
- 収録範囲: 2018年4月以降に新規認可・価格改定された水タバコ製品
- 公開先: <https://flavor-authorization.pages.dev>（Cloudflare Pages、デプロイ後）

## アーキテクチャ

```
GitHub Actions (cron 1h)
  └─ cmd/collect-urls   財務省ページ + Wayback Machine から PDF URL 一覧を生成
  └─ cmd/scraper        各 PDF を Gemini 3 Flash に投げて構造化 JSON 化（キャッシュ）
  └─ cmd/classify       メーカー名を Google 検索 grounding 付きで kiseru/shisha 分類
  └─ data/* を commit
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
- Gemini API Key（<https://aistudio.google.com/apikey>）

### セットアップ
```bash
cp .env.example .env
# .env に GEMINI_API_KEY=... を記入

# Go 側依存解決
go mod download

# フロント依存解決
cd frontend && pnpm install
```

### 主要コマンド
```bash
# 全 PDF を再スクレイプ（フェッチ→Gemini→data/extracted/*.json）
go run ./cmd/scraper

# 既存キャッシュを products.json にマージのみ（API 呼ばない）
go run ./cmd/scraper -skip-extract

# メーカー分類（locked のものは保護される）
go run ./cmd/classify
go run ./cmd/classify -refresh   # 全件再分類（locked は除く）

# メーカー名表記揺れの集約（エイリアスマップ生成）
go run ./cmd/normalize-manufacturers

# フロント開発サーバ
cd frontend && pnpm dev
# http://localhost:3000
```

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
| `GEMINI_API_KEY` | スクレイパ＋分類 | <https://aistudio.google.com/apikey> |
| `CLOUDFLARE_API_TOKEN` | Pages デプロイ | Cloudflare ダッシュボード "My Profile" → "API Tokens" → "Create Token" → "Edit Cloudflare Workers" テンプレで作成 |
| `CLOUDFLARE_ACCOUNT_ID` | Pages デプロイ | Cloudflare ダッシュボード右サイドの "Account ID" |

### 2. Cloudflare Pages プロジェクト作成
- Cloudflare ダッシュボード → **Workers & Pages → Create → Pages → Direct Upload**
- プロジェクト名: **flavor-authorization**
- ビルドはワークフローが行うので、ダッシュボード上は最小設定で構いません

### 3. 初回デプロイ
- main へ push すると `.github/workflows/deploy.yml` が走ります
- Actions タブでログ確認、Secret 漏れなら失敗します
- 成功すると `https://flavor-authorization.pages.dev` で公開されます

### 4. 自動更新 CD
- `.github/workflows/update.yml` が毎時 17 分に発火
- 新しい PDF があれば取り込み → `data/*.json` を commit → deploy.yml 連鎖
- 手動トリガは Actions タブ → "Update tobacco DB" → "Run workflow"

## ライセンス・免責

本プロジェクトは個人プロジェクトであり、財務省・たばこメーカーとの公式関係はありません。データの正確性は保証しません。

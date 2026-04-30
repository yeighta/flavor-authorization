import { ProductBrowser } from '@/components/ProductBrowser';

export default function HomePage() {
  return (
    <div className="mx-auto max-w-6xl px-4 py-8 sm:px-6 lg:px-8">
      <header className="mb-6">
        <h1 className="text-3xl font-bold tracking-tight">認可たばこデータベース</h1>
        <p className="mt-2 text-sm text-stone-600 dark:text-stone-400">
          財務省「製造たばこ小売定価認可」公表PDF（2018年4月以降）から抽出した
          水タバコ（シーシャ）情報をメーカー別に閲覧できます。
        </p>
        <p className="mt-1 text-xs text-stone-500">
          出典:{' '}
          <a
            href="https://www.mof.go.jp/policy/tab_salt/topics/kouriteika.html"
            target="_blank"
            rel="noreferrer"
            className="hover:underline"
          >
            財務省公式ページ
          </a>
          ・分類は AI による自動推定のため誤りが含まれる可能性があります。
        </p>
      </header>

      <ProductBrowser />

      <footer className="mt-12 border-t border-stone-200 pt-6 text-xs text-stone-500 dark:border-stone-800">
        本サイトは個人プロジェクトであり、財務省・たばこメーカーとの公式関係はありません。
      </footer>
    </div>
  );
}

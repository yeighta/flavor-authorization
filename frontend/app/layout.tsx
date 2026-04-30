import type { Metadata } from 'next';
import './globals.css';

export const metadata: Metadata = {
  title: '認可たばこデータベース',
  description: '財務省が認可したパイプたばこ（キセル/水タバコ）のメーカー別一覧。価格・容量・更新日を収録。',
};

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="ja" suppressHydrationWarning>
      <body className="bg-stone-50 text-stone-900 dark:bg-stone-950 dark:text-stone-100">
        {children}
      </body>
    </html>
  );
}

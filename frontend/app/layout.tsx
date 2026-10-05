import type { Metadata, Viewport } from 'next';
import { Big_Shoulders, Zen_Kaku_Gothic_New } from 'next/font/google';
import './globals.css';

// Condensed display face: reads like the stamped names on shisha tins, and fits
// thousands of flavor names into the index without feeling cramped.
const display = Big_Shoulders({
  subsets: ['latin'],
  axes: ['opsz'],
  variable: '--font-display',
  display: 'swap',
});

const sans = Zen_Kaku_Gothic_New({
  weight: ['400', '500', '700'],
  subsets: ['latin'],
  variable: '--font-sans',
  display: 'swap',
  preload: false,
});

const title = '認可たばこデータベース';
const description =
  '財務省が小売定価を認可した水たばこ（シーシャ）のフレーバーを、ブランド・価格・値上げ値下げの履歴とともに引けるデータベース。2018年4月以降の公表PDFから作成。';

export const metadata: Metadata = {
  metadataBase: new URL('https://flavor-authorization.y8a.jp'),
  title,
  description,
  openGraph: {
    title,
    description,
    type: 'website',
    locale: 'ja_JP',
    siteName: title,
    images: [{ url: '/og.png', width: 1200, height: 630, alt: title }],
  },
  twitter: { card: 'summary_large_image', title, description, images: ['/og.png'] },
};

export const viewport: Viewport = {
  themeColor: [
    { media: '(prefers-color-scheme: light)', color: '#f0edf5' },
    { media: '(prefers-color-scheme: dark)', color: '#18111e' },
  ],
};

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="ja" className={`${display.variable} ${sans.variable}`}>
      <body className="font-sans antialiased">{children}</body>
    </html>
  );
}

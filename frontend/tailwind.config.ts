import type { Config } from 'tailwindcss';

const token = (name: string) => `rgb(var(--${name}) / <alpha-value>)`;

const config: Config = {
  content: ['./app/**/*.{ts,tsx}', './components/**/*.{ts,tsx}', './lib/**/*.{ts,tsx}'],
  theme: {
    extend: {
      colors: {
        smoke: token('smoke'),
        glass: token('glass'),
        ink: token('ink'),
        haze: token('haze'),
        veil: token('veil'),
        ember: token('ember'),
        cool: token('cool'),
        family: {
          mint: token('f-mint'),
          citrus: token('f-citrus'),
          berry: token('f-berry'),
          tropical: token('f-tropical'),
          orchard: token('f-orchard'),
          dessert: token('f-dessert'),
          botanical: token('f-botanical'),
          other: token('f-other'),
        },
      },
      fontFamily: {
        display: ['var(--font-display)', 'var(--font-sans)', 'sans-serif'],
        sans: ['var(--font-sans)', 'Hiragino Sans', 'Yu Gothic', 'sans-serif'],
      },
      animation: {
        'sheet-in': 'sheet-in 0.32s cubic-bezier(0.2, 0.8, 0.2, 1) both',
        'veil-in': 'veil-in 0.25s ease-out both',
      },
    },
  },
  plugins: [],
};

export default config;

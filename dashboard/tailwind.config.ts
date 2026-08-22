import type { Config } from 'tailwindcss'

const config: Config = {
  content: [
    './app/**/*.{js,ts,jsx,tsx,mdx}',
    './components/**/*.{js,ts,jsx,tsx,mdx}',
  ],
  theme: {
    extend: {
      colors: {
        canvas: '#f1f4f6',
        surface: '#fbfcfd',
        raised: '#e9eef1',
        seam: '#d5dde3',
        'seam-strong': '#bfcad3',
        ink: '#1a2630',
        body: '#33424e',
        soft: '#5f6f7c',
        faint: '#8b99a4',
        petrol: {
          DEFAULT: '#0f6b87',
          deep: '#0b5168',
        },
      },
    },
  },
  plugins: [],
}
export default config

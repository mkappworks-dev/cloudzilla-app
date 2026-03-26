module.exports = {
  content: [
    "./cmd/server/frontend/templates/**/*.html",
    "./internal/view/**/*.templ",
  ],
  theme: {
    extend: {
      colors: {
        "forge-green":  "#1a7f37",
        "forge-red":    "#cf222e",
        "forge-purple": "#8250df",
        "forge-blue":   "#0969da",
        "forge-dark":   "#24292f",
        "forge-border": "#d0d7de",
        "forge-bg":     "#f6f8fa",
      },
      typography: {
        DEFAULT: {
          css: {
            'code::before': { content: '""' },
            'code::after':  { content: '""' },
          },
        },
      },
    }
  },
  plugins: [
    require('@tailwindcss/typography'),
  ],
}

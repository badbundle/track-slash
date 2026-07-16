/** @type {import('tailwindcss').Config} */
module.exports = {
  content: [
    "./internal/server/templates/**/*.html",
    "./internal/server/**/*.go",
  ],
  darkMode: "media",
  theme: {
    extend: {},
  },
  plugins: [],
};

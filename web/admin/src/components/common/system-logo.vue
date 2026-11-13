<template>
  <div class="app-logo">
    <svg viewBox="0 0 48 48" fill="none" xmlns="http://www.w3.org/2000/svg">
      <defs>
        <linearGradient id="omniflux-logo-gradient" x1="4" y1="42" x2="44" y2="6" gradientUnits="userSpaceOnUse">
          <stop stop-color="var(--logo-color-700)" />
          <stop offset="1" stop-color="var(--logo-color-400)" />
        </linearGradient>
      </defs>
      <circle cx="24" cy="24" r="17" stroke="url(#omniflux-logo-gradient)" stroke-width="4" />
      <path
        d="M9 28c4-9 9-9 15 0s11 9 15 0"
        stroke="url(#omniflux-logo-gradient)"
        stroke-width="4"
        stroke-linecap="round"
      />
    </svg>
  </div>
</template>

<style scoped>
.app-logo {
  --logo-color-400: rgb(var(--primary-400-color));
  --logo-color-700: rgb(var(--primary-700-color));
}
</style>
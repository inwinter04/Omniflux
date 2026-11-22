// @unocss-include
import { getRgb } from '@sa/color';
import { DARK_CLASS } from '@/constants/app';
import { toggleHtmlClass } from '@/utils/common';
import { localStg } from '@/utils/storage';
import { $t } from '@/locales';

const loadingStyles = `<style>
.omniflux-loading-logo {
  animation: omniflux-logo-enter 480ms ease-out both;
}
.omniflux-wave {
  animation: omniflux-wave-drift 3.8s ease-in-out infinite;
}
.omniflux-wave-2 {
  animation-delay: -1.4s;
  animation-duration: 4.6s;
}
.omniflux-wave-3 {
  animation-delay: -2.6s;
  animation-duration: 5.2s;
}
.omniflux-wave-4 {
  animation-delay: -3.4s;
  animation-duration: 5.8s;
}
@keyframes omniflux-logo-enter {
  from { opacity: 0; transform: scale(.96); }
  to { opacity: 1; transform: scale(1); }
}
@keyframes omniflux-wave-drift {
  0%, 100% { transform: translateX(-1.5px); }
  50% { transform: translateX(1.5px); }
}
@media (prefers-reduced-motion: reduce) {
  .omniflux-loading-logo,
  .omniflux-wave {
    animation: none;
  }
}
</style>`;

export function setupLoading() {
  const themeColor = localStg.get('themeColor') || '#646cff';
  const darkMode = localStg.get('darkMode') || false;
  const { r, g, b } = getRgb(themeColor);
  const primaryColor = `--primary-color: ${r} ${g} ${b}`;

  if (darkMode) {
    toggleHtmlClass(DARK_CLASS).add();
  }

  const loading = `
<div class="fixed-center flex-col bg-layout" style="${primaryColor}">
  ${loadingStyles}
  <div class="w-128px h-128px">
    ${getLogoSvg()}
  </div>
  <h2 class="mt-28px text-28px font-500 text-primary">${$t('system.title')}</h2>
</div>`;

  const app = document.getElementById('app');

  if (app) {
    app.innerHTML = loading;
  }
}

function getLogoSvg() {
  return `<svg
    class="omniflux-loading-logo"
    width="100%"
    height="100%"
    viewBox="0 0 128 128"
    xmlns="http://www.w3.org/2000/svg"
    role="img"
    aria-label="Omniflux"
  >
    <defs>
      <linearGradient id="omniflux-loading-morning" x1="64" y1="0" x2="64" y2="128" gradientUnits="userSpaceOnUse">
        <stop stop-color="#F3F8FC" />
        <stop offset="1" stop-color="#CDE5F2" />
      </linearGradient>
      <clipPath id="omniflux-loading-disc">
        <circle cx="64" cy="64" r="62" />
      </clipPath>
    </defs>
    <circle cx="64" cy="64" r="62" fill="url(#omniflux-loading-morning)" />
    <g clip-path="url(#omniflux-loading-disc)">
      <circle cx="46" cy="50" r="25.5" fill="none" stroke="#247FA7" stroke-width="9.5" />
      <path
        d="M26.47 33.61A25.5 25.5 0 0 1 58.75 27.92"
        fill="none"
        stroke="#72BCD6"
        stroke-width="3.5"
        stroke-linecap="round"
      />
      <g transform="translate(70 80) scale(.03808594 -.03808594)" fill="#174F70">
        <path d="M929 1271Q848 1307 790 1307Q646 1307 592 1071H838L813 870H561L401 -68Q375 -221 260 -320Q145 -419 -9 -420L-62 -254Q9 -238 66 -183Q123 -128 136 -58L303 870H152L185 1071H333Q436 1510 802 1510Q906 1510 979 1467Z" />
      </g>
      <g class="omniflux-wave omniflux-wave-1">
        <path d="M-9 87C13 68 34 68 54 81C75 95 95 96 114 81C124 72 134 70 141 71V140H-9Z" fill="#99D2E1" />
        <path d="M-9 86C13 67 34 67 54 80C75 94 95 95 114 80C124 71 134 69 141 70" fill="none" stroke="#F2F8FB" stroke-width="2.25" />
      </g>
      <g class="omniflux-wave omniflux-wave-2">
        <path d="M-10 102C13 83 36 84 56 97C77 111 98 110 117 95C126 87 135 85 142 86V140H-10Z" fill="#56AEC8" />
        <path d="M-10 101C13 82 36 83 56 96C77 110 98 109 117 94C126 86 135 84 142 85" fill="none" stroke="#CDE7F0" stroke-width="1.75" />
      </g>
      <g class="omniflux-wave omniflux-wave-3">
        <path d="M-9 118C15 100 37 101 58 113C79 126 100 124 119 111C128 104 136 102 142 103V140H-9Z" fill="#2C86A8" />
      </g>
      <g class="omniflux-wave omniflux-wave-4">
        <path d="M-9 132C16 115 39 116 60 127C82 139 103 137 121 125C130 119 137 117 143 118V140H-9Z" fill="#1B5F83" />
      </g>
    </g>
    <circle cx="64" cy="64" r="61.4" fill="none" stroke="#FFFFFF" stroke-opacity="0.86" stroke-width="1.2" />
  </svg>`;
}

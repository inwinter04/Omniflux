import type { GlobalThemeOverrides } from 'naive-ui';
import { defu } from 'defu';
import { addColorAlpha, getColorPalette, getPaletteColorByNumber, getRgb } from '@sa/color';
import { DARK_CLASS } from '@/constants/app';
import { toggleHtmlClass } from '@/utils/common';
import { localStg } from '@/utils/storage';
import { overrideThemeSettings, themeSettings } from '@/theme/settings';
import { themeVars } from '@/theme/vars';

/** Init theme settings */
export function initThemeSettings() {
  const isProd = import.meta.env.PROD;

  // if it is development mode, the theme settings will not be cached, by update `themeSettings` in `src/theme/settings.ts` to update theme settings
  if (!isProd) return themeSettings;

  // if it is production mode, the theme settings will be cached in localStorage
  // if want to update theme settings when publish new version, please update `overrideThemeSettings` in `src/theme/settings.ts`

  const localSettings = localStg.get('themeSettings');

  let settings = defu(localSettings, themeSettings);

  const isOverride = localStg.get('overrideThemeFlag') === BUILD_TIME;

  if (!isOverride) {
    settings = defu(overrideThemeSettings, settings);

    localStg.set('overrideThemeFlag', BUILD_TIME);
  }

  return settings;
}

/**
 * create theme token css vars value by theme settings
 *
 * @param colors Theme colors
 * @param tokens Theme setting tokens
 * @param [recommended=false] Use recommended color. Default is `false`
 */
export function createThemeToken(
  colors: App.Theme.ThemeColor,
  tokens?: App.Theme.ThemeSetting['tokens'],
  recommended = false
) {
  const paletteColors = createThemePaletteColors(colors, recommended);

  const { light, dark } = tokens || themeSettings.tokens;

  const themeTokens: App.Theme.ThemeTokenCSSVars = {
    colors: {
      ...paletteColors,
      nprogress: paletteColors.primary,
      ...light.colors
    },
    boxShadow: {
      ...light.boxShadow
    }
  };

  const darkThemeTokens: App.Theme.ThemeTokenCSSVars = {
    colors: {
      ...themeTokens.colors,
      // 暗色背景下换用提亮后的主色,text-primary 这类文字才看得清
      primary: getDarkPrimaryColor(colors.primary, recommended),
      ...dark?.colors
    },
    boxShadow: {
      ...themeTokens.boxShadow,
      ...dark?.boxShadow
    }
  };

  return {
    themeTokens,
    darkThemeTokens
  };
}

/**
 * Create theme palette colors
 *
 * @param colors Theme colors
 * @param [recommended=false] Use recommended color. Default is `false`
 */
function createThemePaletteColors(colors: App.Theme.ThemeColor, recommended = false) {
  const colorKeys = Object.keys(colors) as App.Theme.ThemeColorKey[];
  const colorPaletteVar = {} as App.Theme.ThemePaletteColor;

  colorKeys.forEach(key => {
    const colorMap = getColorPalette(colors[key], recommended);

    colorPaletteVar[key] = colorMap.get(500)!;

    colorMap.forEach((hex, number) => {
      colorPaletteVar[`${key}-${number}`] = hex;
    });
  });

  return colorPaletteVar;
}

/**
 * Get css var by tokens
 *
 * @param tokens Theme base tokens
 */
function getCssVarByTokens(tokens: App.Theme.BaseToken) {
  const styles: string[] = [];

  function removeVarPrefix(value: string) {
    return value.replace('var(', '').replace(')', '');
  }

  function removeRgbPrefix(value: string) {
    return value.replace('rgb(', '').replace(')', '');
  }

  for (const [key, tokenValues] of Object.entries(themeVars)) {
    for (const [tokenKey, tokenValue] of Object.entries(tokenValues)) {
      let cssVarsKey = removeVarPrefix(tokenValue);
      let cssValue = tokens[key][tokenKey];

      if (key === 'colors') {
        cssVarsKey = removeRgbPrefix(cssVarsKey);
        const { r, g, b } = getRgb(cssValue);
        cssValue = `${r} ${g} ${b}`;
      }

      styles.push(`${cssVarsKey}: ${cssValue}`);
    }
  }

  const styleStr = styles.join(';');

  return styleStr;
}

/**
 * Add theme vars to global
 *
 * @param tokens
 */
export function addThemeVarsToGlobal(tokens: App.Theme.BaseToken, darkTokens: App.Theme.BaseToken) {
  const cssVarStr = getCssVarByTokens(tokens);
  const darkCssVarStr = getCssVarByTokens(darkTokens);

  const css = `
    :root {
      ${cssVarStr}
    }
  `;

  const darkCss = `
    html.${DARK_CLASS} {
      ${darkCssVarStr}
    }
  `;

  const styleId = 'theme-vars';

  const style = document.querySelector(`#${styleId}`) || document.createElement('style');

  style.id = styleId;

  style.textContent = css + darkCss;

  document.head.appendChild(style);
}

/**
 * Toggle css dark mode
 *
 * @param darkMode Is dark mode
 */
export function toggleCssDarkMode(darkMode = false) {
  const { add, remove } = toggleHtmlClass(DARK_CLASS);

  if (darkMode) {
    add();
  } else {
    remove();
  }
}

/**
 * Toggle auxiliary color modes
 *
 * @param grayscaleMode
 * @param colourWeakness
 */
export function toggleAuxiliaryColorModes(grayscaleMode = false, colourWeakness = false) {
  const htmlElement = document.documentElement;
  htmlElement.style.filter = [grayscaleMode ? 'grayscale(100%)' : '', colourWeakness ? 'invert(80%)' : '']
    .filter(Boolean)
    .join(' ');
}

type NaiveColorScene = '' | 'Suppl' | 'Hover' | 'Pressed' | 'Active';
type NaiveColorKey = `${App.Theme.ThemeColorKey}Color${NaiveColorScene}`;
type NaiveThemeColor = Partial<Record<NaiveColorKey, string>>;
interface NaiveColorAction {
  scene: NaiveColorScene;
  handler: (color: string) => string;
}

/**
 * Get naive theme colors
 *
 * @param colors Theme colors
 * @param [recommended=false] Use recommended color. Default is `false`
 */
function getNaiveThemeColors(colors: App.Theme.ThemeColor, recommended = false) {
  const colorActions: NaiveColorAction[] = [
    { scene: '', handler: color => color },
    { scene: 'Suppl', handler: color => color },
    { scene: 'Hover', handler: color => getPaletteColorByNumber(color, 500, recommended) },
    { scene: 'Pressed', handler: color => getPaletteColorByNumber(color, 700, recommended) },
    { scene: 'Active', handler: color => addColorAlpha(color, 0.1) }
  ];

  const themeColors: NaiveThemeColor = {};

  const colorEntries = Object.entries(colors) as [App.Theme.ThemeColorKey, string][];

  colorEntries.forEach(color => {
    colorActions.forEach(action => {
      const [colorType, colorValue] = color;
      const colorKey: NaiveColorKey = `${colorType}Color${action.scene}`;
      themeColors[colorKey] = action.handler(colorValue);
    });
  });

  return themeColors;
}

/** Naive UI 暗色主题的弹层底色,是暗色模式下最亮的承载面(下拉选项、菜单里的主色文字落在它上面) */
const NAIVE_DARK_POPOVER_COLOR = 'rgb(72, 72, 78)';

/** 文字与背景的最低对比度(WCAG AA,正文文字) */
const MIN_TEXT_CONTRAST = 4.5;

/** Get the contrast ratio of two colors */
function getContrastRatio(colorA: ReturnType<typeof getRgb>, colorB: ReturnType<typeof getRgb>) {
  function getLuminance({ r, g, b }: ReturnType<typeof getRgb>) {
    const channel = (value: number) => {
      const normalized = value / 255;

      return normalized <= 0.03928 ? normalized / 12.92 : ((normalized + 0.055) / 1.055) ** 2.4;
    };

    return 0.2126 * channel(r) + 0.7152 * channel(g) + 0.0722 * channel(b);
  }

  const luminanceA = getLuminance(colorA);
  const luminanceB = getLuminance(colorB);

  return (Math.max(luminanceA, luminanceB) + 0.05) / (Math.min(luminanceA, luminanceB) + 0.05);
}

/**
 * Get the palette number of the primary color for dark mode
 *
 * 暗色模式下主色会作为文字色落在深色背景上(文字按钮、菜单选中项、下拉选项等),原色太暗看不清。
 * 从原色开始逐级取更浅的色阶,取第一个在 Naive UI 暗色弹层底色上对比度达标的色阶。
 *
 * @param color The primary color
 * @param [recommended=false] Use recommended color palette. Default is `false`
 */
function getDarkPrimaryPaletteNumber(color: string, recommended = false) {
  const paletteNumbers: App.Theme.ColorPaletteNumber[] = [500, 400, 300, 200, 100];
  const popoverColor = getRgb(NAIVE_DARK_POPOVER_COLOR);

  for (const number of paletteNumbers) {
    const paletteColor = getRgb(getPaletteColorByNumber(color, number, recommended));

    if (getContrastRatio(paletteColor, popoverColor) >= MIN_TEXT_CONTRAST) return number;
  }

  return 50;
}

/**
 * Get the primary color for dark mode
 *
 * @param color The primary color
 * @param [recommended=false] Use recommended color palette. Default is `false`
 */
export function getDarkPrimaryColor(color: string, recommended = false) {
  return getPaletteColorByNumber(color, getDarkPrimaryPaletteNumber(color, recommended), recommended);
}

/**
 * Get the NaiveUI primary colors for dark mode
 *
 * 暗色模式下主色(“信息色跟随主色”开启时连同信息色)换成提亮后的色阶:
 * 悬停色与基准色相同、按下时深两档,与浅色模式下的深浅关系一致。
 *
 * @param colors Theme colors
 * @param [recommended=false] Use recommended color palette. Default is `false`
 */
function getNaiveDarkPrimaryColors(colors: App.Theme.ThemeColor, recommended = false) {
  const { primary, info } = colors;
  const number = getDarkPrimaryPaletteNumber(primary, recommended);
  const pressedNumber = (number + 200) as App.Theme.ColorPaletteNumber;

  // “信息色跟随主色”开启时,信息色与主色相同,需要一并提亮
  const colorTypes: App.Theme.ThemeColorKey[] = info === primary ? ['primary', 'info'] : ['primary'];

  const themeColors: NaiveThemeColor = {};

  colorTypes.forEach(colorType => {
    const color = colors[colorType];
    const baseColor = getPaletteColorByNumber(color, number, recommended);

    themeColors[`${colorType}Color`] = baseColor;
    themeColors[`${colorType}ColorSuppl`] = baseColor;
    themeColors[`${colorType}ColorHover`] = baseColor;
    themeColors[`${colorType}ColorPressed`] = getPaletteColorByNumber(color, pressedNumber, recommended);
    themeColors[`${colorType}ColorActive`] = addColorAlpha(baseColor, 0.1);
  });

  return themeColors;
}

/**
 * Get naive theme
 *
 * @param colors Theme colors
 * @param settings Theme settings object
 * @param overrides Optional manual overrides from preset
 * @param [dark=false] Dark mode. Default is `false`
 */
export function getNaiveTheme(
  colors: App.Theme.ThemeColor,
  settings: App.Theme.ThemeSetting,
  overrides?: GlobalThemeOverrides,
  dark = false
) {
  const { primary: colorLoading } = colors;

  const theme: GlobalThemeOverrides = {
    common: {
      ...getNaiveThemeColors(colors, settings.recommendColor),
      ...(dark ? getNaiveDarkPrimaryColors(colors, settings.recommendColor) : {}),
      borderRadius: `${settings.themeRadius}px`
    },
    LoadingBar: {
      colorLoading
    },
    Tag: {
      borderRadius: `${settings.themeRadius}px`
    }
  };

  // If there are overrides, merge them with priority
  // overrides has higher priority than auto-generated theme
  return overrides ? defu(overrides, theme) : theme;
}

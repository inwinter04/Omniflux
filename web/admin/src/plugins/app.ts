import { h } from 'vue';
import type { App } from 'vue';
import { NButton, NSelect } from 'naive-ui';
import { $t } from '@/locales';

/**
 * Setup the default props of NaiveUI components
 *
 * 下拉选项比菜单宽时只显示省略号,认不出是哪一项。Ant Design、TDesign 的做法是菜单保持与输入框等宽,
 * 给每个选项加上原生 title,悬停就能看到完整名称。这里通过 node-props 的默认值对所有下拉统一生效,
 * 单个下拉自己传了 node-props 时不受影响。需要在应用挂载前执行。
 */
export function setupNaiveComponentDefaults() {
  const selectProps = (NSelect as unknown as { props: { nodeProps: unknown } }).props;

  // Naive UI 把这一项写成简写形式(nodeProps: Function),Vue 第一次用到时会复制成新的定义,
  // 所以要在应用挂载前把这一项整个替换掉。只有文字类型的选项才加 title,自定义渲染的选项由使用方自己决定。
  selectProps.nodeProps = {
    type: Function,
    default: (option: { label?: unknown }) => (typeof option.label === 'string' ? { title: option.label } : {})
  };
}

export function setupAppErrorHandle(app: App) {
  app.config.errorHandler = (err, vm, info) => {
    // eslint-disable-next-line no-console
    console.error(err, vm, info);
  };
}

export function setupAppVersionNotification() {
  // Update check interval in milliseconds
  const UPDATE_CHECK_INTERVAL = 3 * 60 * 1000;

  const canAutoUpdateApp = import.meta.env.VITE_AUTOMATICALLY_DETECT_UPDATE === 'Y' && import.meta.env.PROD;
  if (!canAutoUpdateApp) return;

  let isShow = false;
  let updateInterval: ReturnType<typeof setInterval> | undefined;

  const checkForUpdates = async () => {
    if (isShow) return;

    const buildTime = await getHtmlBuildTime();

    // If failed to get build time or build time hasn't changed, no update is needed.
    if (!buildTime || buildTime === BUILD_TIME) {
      return;
    }

    isShow = true;

    // Show update notification
    const n = window.$notification?.create({
      title: $t('system.updateTitle'),
      content: $t('system.updateContent'),
      action() {
        return h('div', { style: { display: 'flex', justifyContent: 'end', gap: '12px', width: '325px' } }, [
          h(
            NButton,
            {
              onClick() {
                n?.destroy();
                isShow = false;
              }
            },
            () => $t('system.updateCancel')
          ),
          h(
            NButton,
            {
              type: 'primary',
              onClick() {
                location.reload();
              }
            },
            () => $t('system.updateConfirm')
          )
        ]);
      },
      onClose() {
        isShow = false;
      }
    });
  };

  const startUpdateInterval = () => {
    if (updateInterval) {
      clearInterval(updateInterval);
    }
    updateInterval = setInterval(checkForUpdates, UPDATE_CHECK_INTERVAL);
  };

  // If updates should be checked, set up the visibility change listener and start the update interval
  if (!isShow && document.visibilityState === 'visible') {
    // Check for updates when the document is visible
    document.addEventListener('visibilitychange', () => {
      if (document.visibilityState === 'visible') {
        checkForUpdates();
        startUpdateInterval();
      }
    });

    // Start the update interval
    startUpdateInterval();
  }
}

async function getHtmlBuildTime(): Promise<string | null> {
  const baseUrl = import.meta.env.VITE_BASE_URL || '/';

  try {
    const res = await fetch(`${baseUrl}index.html?time=${Date.now()}`);

    if (!res.ok) {
      return null;
    }

    const html = await res.text();
    const match = html.match(/<meta name="buildTime" content="(.*)">/);
    return match?.[1] || null;
  } catch (error) {
    window.console.error('getHtmlBuildTime error:', error);
    return null;
  }
}

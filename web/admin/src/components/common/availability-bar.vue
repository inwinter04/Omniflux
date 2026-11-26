<script setup lang="ts">
import { computed } from 'vue';
import { NTooltip } from 'naive-ui';
import { $t, getLocale } from '@/locales';
import { healthStatusText, probeReasonText } from '@/utils/admin-status';

defineOptions({ name: 'AvailabilityBar' });

interface Props {
  buckets?: Api.Management.HeartbeatBucket[];
  compact?: boolean;
}

const props = withDefaults(defineProps<Props>(), {
  buckets: () => [],
  compact: false
});

const normalizedBuckets = computed<Api.Management.HeartbeatBucket[]>(() => {
  if (props.buckets && props.buckets.length > 0) return props.buckets;
  const end = new Date();
  end.setMinutes(0, 0, 0);
  return Array.from({ length: 24 }, (_, idx) => {
    const t = new Date(end.getTime() - (23 - idx) * 3600 * 1000);
    return {
      bucket_start: t.toISOString(),
      status: 'no_data',
      total: 0,
      ok_count: 0,
      avg_latency_ms: null
    };
  });
});

function barColor(status: Api.Management.HeartbeatBucket['status']) {
  switch (status) {
    case 'healthy':
      return '#18a058';
    case 'degraded':
      return '#f0a020';
    case 'unhealthy':
      return '#d03050';
    default:
      return 'rgba(156, 163, 175, 0.28)';
  }
}

function formatHourRange(iso: string) {
  const start = new Date(iso);
  const end = new Date(start.getTime() + 3600 * 1000);
  const locale = getLocale();
  const fmt: Intl.DateTimeFormatOptions = { hour: '2-digit', minute: '2-digit' };
  return `${start.toLocaleTimeString(locale, fmt)} - ${end.toLocaleTimeString(locale, fmt)}`;
}

function bucketRateText(bucket: Api.Management.HeartbeatBucket) {
  if (bucket.total === 0) return $t('business.no-data');
  const pct = Math.round((bucket.ok_count / bucket.total) * 1000) / 10;
  return `${pct}% (${bucket.ok_count}/${bucket.total})`;
}
</script>

<template>
  <div class="inline-flex items-center" :class="compact ? 'gap-2px' : 'gap-3px'">
    <NTooltip v-for="(bucket, index) in normalizedBuckets" :key="bucket.bucket_start + '-' + index" trigger="hover">
      <template #trigger>
        <span
          class="inline-block rounded-2px transition-transform duration-150 hover:scale-y-125"
          :class="compact ? 'h-14px w-4px' : 'h-20px w-6px'"
          :style="{ backgroundColor: barColor(bucket.status) }"
        />
      </template>
      <div class="flex flex-col gap-2px text-12px">
        <div class="font-medium">{{ formatHourRange(bucket.bucket_start) }}</div>
        <div>
          {{ healthStatusText(bucket.status) }} · {{ bucketRateText(bucket) }}
        </div>
        <div v-if="bucket.avg_latency_ms !== null">
          {{ $t('business.latencyMs', { value: bucket.avg_latency_ms }) }}
        </div>
        <div v-if="bucket.last_reason" class="text-#fca5a5">
          {{ probeReasonText(bucket.last_reason) }}
        </div>
      </div>
    </NTooltip>
  </div>
</template>

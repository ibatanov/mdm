<script setup lang="ts">
import { computed } from 'vue'

const props = withDefaults(
  defineProps<{
    value: unknown
    label?: string
    open?: boolean
  }>(),
  {
    label: 'JSON',
    open: false,
  },
)

const pretty = computed(() => JSON.stringify(props.value, null, 2))
</script>

<template>
  <details class="json-box" :open="open">
    <summary>{{ label }}</summary>
    <pre>{{ pretty }}</pre>
  </details>
</template>

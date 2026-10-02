<script>
  import { humanBytes } from '$lib/format';

  export let candidate;
  export let name = '';
  export let reasons = [];
  export let showScore = false;
  export let showReasons = false;
</script>

<div class="candidate-row" class:attention-candidate={$$slots.actions}>
  <div>
    <strong>{name}</strong>
    <small>{candidate.size_bytes ? humanBytes(candidate.size_bytes) : 'Size unavailable'}{#if showScore && candidate.score != null} · score {candidate.score}{/if}</small>
  </div>
  <span class:accepted={candidate.accepted !== false} class="candidate-decision">{candidate.exact === false ? 'Alternative' : 'Exact'}</span>
  {#if showReasons}
    <div class="candidate-reasons">{#each reasons as reason}<span class:negative={reason.trim().startsWith('-')}>{reason}</span>{/each}</div>
  {/if}
  <slot name="actions" />
</div>

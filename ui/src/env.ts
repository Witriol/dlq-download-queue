import { defineEnvVars } from '@sveltejs/kit/env';

const optionalString = (value: string | undefined) => value;

export const variables = defineEnvVars({
  DLQ_API_BASE: { schema: optionalString },
  DLQ_API: { schema: optionalString }
});

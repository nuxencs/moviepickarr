import {
  INTEGRATION_RUN_RESULT_STATUSES,
  type IntegrationRunOperation,
  type IntegrationRunResultStatus,
} from "@/api/integrations";

export interface AdminRunsSearch {
  integration?: string;
  operation?: IntegrationRunOperation;
  status?: IntegrationRunResultStatus;
  cursor?: string;
}

const STATUSES = new Set<IntegrationRunResultStatus>(INTEGRATION_RUN_RESULT_STATUSES);
const IDENTIFIER = /^[a-z][a-z0-9_-]{0,63}$/;
const CURSOR = /^(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z),([1-9]\d{0,18})$/;
const MAX_CURSOR_ID = 9_223_372_036_854_775_807n;

function allowed<T extends string>(value: unknown, values: Set<T>): T | undefined {
  return typeof value === "string" && values.has(value as T) ? (value as T) : undefined;
}

export function validateAdminRunsSearch(search: Record<string, unknown>): AdminRunsSearch {
  const result: AdminRunsSearch = {};
  if (typeof search.integration === "string" && IDENTIFIER.test(search.integration)) {
    result.integration = search.integration;
  }
  if (typeof search.operation === "string" && IDENTIFIER.test(search.operation)) {
    result.operation = search.operation;
  }
  const status = allowed(search.status, STATUSES);
  if (status) result.status = status;
  if (typeof search.cursor === "string") {
    const match = CURSOR.exec(search.cursor);
    if (match) {
      const timestamp = new Date(match[1]);
      const canonicalTimestamp = `${match[1].slice(0, -1)}.000Z`;
      if (
        !Number.isNaN(timestamp.getTime()) &&
        timestamp.toISOString() === canonicalTimestamp &&
        BigInt(match[2]) <= MAX_CURSOR_ID
      ) {
        result.cursor = search.cursor;
      }
    }
  }
  return result;
}

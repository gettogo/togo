// Live Activity request parsing and APNs payload. Live Activity pushes carry
// metadata, plus ctx: the activity's context sealed on the host for the one
// device, which the relay passes through unread.

export const LIVE_EVENTS = ["update", "start", "end"] as const;
export type LiveEvent = (typeof LIVE_EVENTS)[number];

const MAX_STATE_STRING = 64;
const MAX_COMMAND = 160;
// The sealed context, base64url without padding. APNs takes 4 KB for the
// whole payload; the helper keeps the envelope to 2048 characters.
const MAX_CTX = 2400;
const CTX_RE = /^[A-Za-z0-9_-]+$/;
// staleDate and dismissalDate must fall within [now - 60 s, now + 8 h].
const DATE_PAST_SLACK = 60;
const DATE_MAX_AHEAD = 8 * 3600;
const STATUSES = ["needs_you", "working", "done", "ended", "failed", "offline"] as const;
const EXIT_REASONS = ["exit", "interrupted", "error", "usage_limit"] as const;
const REQUEST_ID_RE = /^r_[a-z2-7]{10}$/;
const HOST_ID_RE = /^h_[a-z2-7]{26}$/;
const ATTRIBUTES_TYPE = "TogoActivityAttributes";

export class FieldError extends Error {
  constructor(readonly field: string) {
    super(field);
  }
}

export interface ContentState {
  bib: string | null;
  agent: string;
  status: (typeof STATUSES)[number];
  waiting: number;
  since: number;
  requestId?: string | null;
  command?: string | null;
  reason?: (typeof EXIT_REASONS)[number];
  ctx?: string;
}

export interface LiveActivity {
  event: LiveEvent;
  contentState: ContentState;
  attributes?: { hostId: string };
  staleDate?: number;
  dismissalDate?: number;
  urgent?: boolean;
  alert?: boolean;
}

export function parseLiveActivity(raw: unknown): LiveActivity {
  if (!isObject(raw)) throw new FieldError("liveActivity");
  const event = raw.event;
  if (typeof event !== "string" || !(LIVE_EVENTS as readonly string[]).includes(event)) {
    throw new FieldError("liveActivity.event");
  }
  const cs = raw.contentState;
  if (!isObject(cs)) throw new FieldError("liveActivity.contentState");
  const state: ContentState = {
    bib: cs.bib === null ? null : boundedString(cs.bib, "bib"),
    agent: boundedString(cs.agent, "agent"),
    waiting: nonNegativeInt(cs.waiting, "waiting"),
    since: nonNegativeInt(cs.since, "since"),
    status: parseStatus(cs.status),
  };
  if (cs.requestId !== undefined) {
    if (cs.requestId !== null && !(typeof cs.requestId === "string" && REQUEST_ID_RE.test(cs.requestId))) {
      throw new FieldError("liveActivity.contentState.requestId");
    }
    state.requestId = cs.requestId;
  }
  if (cs.command !== undefined) {
    if (cs.command !== null && !(typeof cs.command === "string" && cs.command.length >= 1 && cs.command.length <= MAX_COMMAND)) {
      throw new FieldError("liveActivity.contentState.command");
    }
    state.command = cs.command;
  }
  if (cs.reason !== undefined) {
    // Only a stopped agent has an exit reason, and only the fixed words may reach the device.
    if (
      !(typeof cs.reason === "string" && (EXIT_REASONS as readonly string[]).includes(cs.reason)) ||
      (state.status !== "ended" && state.status !== "failed")
    ) {
      throw new FieldError("liveActivity.contentState.reason");
    }
    state.reason = cs.reason as (typeof EXIT_REASONS)[number];
  }

  if (cs.ctx !== undefined) {
    if (!(typeof cs.ctx === "string" && cs.ctx.length >= 1 && cs.ctx.length <= MAX_CTX && CTX_RE.test(cs.ctx))) {
      throw new FieldError("liveActivity.contentState.ctx");
    }
    state.ctx = cs.ctx;
  }

  const la: LiveActivity = { event: event as LiveEvent, contentState: state };
  if (event === "start") {
    const attrs = raw.attributes;
    if (!isObject(attrs) || typeof attrs.hostId !== "string" || !HOST_ID_RE.test(attrs.hostId)) {
      throw new FieldError("liveActivity.attributes");
    }
    la.attributes = { hostId: attrs.hostId };
  } else if (raw.attributes !== undefined) {
    throw new FieldError("liveActivity.attributes");
  }
  if (raw.urgent !== undefined) {
    if (typeof raw.urgent !== "boolean") throw new FieldError("liveActivity.urgent");
    la.urgent = raw.urgent;
  }
  if (raw.alert !== undefined) {
    // An update that stands in for an alert push: it lights the screen and sounds.
    if (typeof raw.alert !== "boolean" || event !== "update") throw new FieldError("liveActivity.alert");
    la.alert = raw.alert;
  }
  if (raw.staleDate !== undefined) la.staleDate = unixSeconds(raw.staleDate, "staleDate");
  if (raw.dismissalDate !== undefined) {
    if (event !== "end") throw new FieldError("liveActivity.dismissalDate");
    la.dismissalDate = unixSeconds(raw.dismissalDate, "dismissalDate");
  }
  return la;
}

// Statuses a person may act on or wants to see at once; anything else is routine progress.
const URGENT_STATUSES: readonly ContentState["status"][] = ["needs_you", "done", "ended", "failed", "offline"];

/**
 * APNs priority for a Live Activity push. Routine progress (a working agent's step) goes at
 * priority 5, so a long turn does not spend the device's priority-10 budget and get the
 * needs-you update that matters throttled. Starts, ends, and states the person acts on go
 * at 10. A sender may mark an update urgent, such as the one that clears an answered request.
 */
export function liveActivityPriority(la: LiveActivity): "5" | "10" {
  if (la.event !== "update" || la.urgent) return "10";
  return URGENT_STATUSES.includes(la.contentState.status) ? "10" : "5";
}

const ALERT_WORDS: Record<ContentState["status"], string> = {
  needs_you: "Needs you",
  working: "Working",
  done: "Done",
  ended: "Ended",
  failed: "Failed",
  offline: "Offline",
};

export function liveActivityPayload(la: LiveActivity, nowSeconds: number): unknown {
  const aps: Record<string, unknown> = {
    timestamp: nowSeconds,
    event: la.event,
    "content-state": la.contentState,
  };
  if (la.event === "start") {
    aps["attributes-type"] = ATTRIBUTES_TYPE;
    aps.attributes = la.attributes;
    aps.alert = { title: "Togo", body: "Needs you", sound: "default" };
  }
  if (la.event === "update" && la.alert) {
    // Fixed words from the plain status only: nothing sealed reaches the alert.
    aps.alert = { title: "Togo", body: ALERT_WORDS[la.contentState.status], sound: "default" };
  }
  if (la.staleDate !== undefined) aps["stale-date"] = la.staleDate;
  if (la.dismissalDate !== undefined) aps["dismissal-date"] = la.dismissalDate;
  return { aps };
}

function parseStatus(v: unknown): ContentState["status"] {
  if (typeof v !== "string" || !(STATUSES as readonly string[]).includes(v)) {
    throw new FieldError("liveActivity.contentState.status");
  }
  return v as ContentState["status"];
}

function boundedString(v: unknown, name: string): string {
  if (typeof v !== "string" || v.length === 0 || v.length > MAX_STATE_STRING) {
    throw new FieldError(`liveActivity.contentState.${name}`);
  }
  return v;
}

function nonNegativeInt(v: unknown, name: string): number {
  if (typeof v !== "number" || !Number.isSafeInteger(v) || v < 0) {
    throw new FieldError(`liveActivity.contentState.${name}`);
  }
  return v;
}

function unixSeconds(v: unknown, name: string): number {
  const now = Math.floor(Date.now() / 1000);
  if (typeof v !== "number" || !Number.isSafeInteger(v) || v < now - DATE_PAST_SLACK || v > now + DATE_MAX_AHEAD) {
    throw new FieldError(`liveActivity.${name}`);
  }
  return v;
}

function isObject(v: unknown): v is Record<string, unknown> {
  return typeof v === "object" && v !== null && !Array.isArray(v);
}

import { Router } from "./http";
import { handleMailbox } from "./mailbox";
import { handlePush } from "./push";

export { Mailbox } from "./mailbox";
export { RateLimiter } from "./ratelimit";

const router = new Router()
  .route("GET", "/v1/health", () => new Response("ok", { headers: { "content-type": "text/plain" } }))
  .route("POST", "/v1/push", handlePush);

export default {
  fetch: (request, env) =>
    new URL(request.url).pathname.startsWith("/v1/mailbox/") ? handleMailbox(request, env) : router.handle(request, env),
} satisfies ExportedHandler<Env>;

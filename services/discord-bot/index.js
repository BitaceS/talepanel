import Fastify from "fastify";
import crypto from "node:crypto";

const {
  DISCORD_TOKEN,
  GITHUB_WEBHOOK_SECRET,
  CH_RELEASES,
  CH_PULL_REQUESTS,
  CH_COMMITS,
  CH_BUG_REPORTS,
  PORT = "3030",
} = process.env;

if (!DISCORD_TOKEN) throw new Error("DISCORD_TOKEN missing");
if (!GITHUB_WEBHOOK_SECRET) throw new Error("GITHUB_WEBHOOK_SECRET missing");

const DISCORD_API = "https://discord.com/api/v10";
const COLOR = {
  release: 0x6f4cff,
  pr_open: 0x238636,
  pr_merged: 0x8957e5,
  pr_closed: 0xda3633,
  push: 0x58a6ff,
  issue_open: 0xf85149,
  issue_closed: 0x8957e5,
  star: 0xf2cc60,
};

const app = Fastify({ logger: true });

app.get("/health", async () => ({ ok: true }));

// Verify GitHub HMAC, then handle the event.
app.post("/webhook/github", {
  config: { rawBody: true },
}, async (req, reply) => {
  const sig = req.headers["x-hub-signature-256"];
  const event = req.headers["x-github-event"];
  const raw = req.rawBody || JSON.stringify(req.body);

  const expected = "sha256=" + crypto
    .createHmac("sha256", GITHUB_WEBHOOK_SECRET)
    .update(raw)
    .digest("hex");

  if (!sig || !crypto.timingSafeEqual(Buffer.from(sig), Buffer.from(expected))) {
    return reply.code(401).send({ error: "bad signature" });
  }

  const body = req.body;
  try {
    await dispatch(event, body);
  } catch (err) {
    req.log.error({ err }, "dispatch failed");
    return reply.code(500).send({ error: "dispatch failed" });
  }
  return { ok: true };
});

async function dispatch(event, b) {
  if (event === "ping") return;

  if (event === "release" && b.action === "published") {
    return postEmbed(CH_RELEASES, {
      title: `🎁 ${b.release.name || b.release.tag_name}`,
      url: b.release.html_url,
      description: trim(b.release.body, 1500) || "_(no release notes)_",
      color: COLOR.release,
      author: authorOf(b.release.author),
      footer: { text: `${b.repository.full_name} • ${b.release.tag_name}` },
      timestamp: b.release.published_at,
    });
  }

  if (event === "pull_request") {
    const pr = b.pull_request;
    let color = COLOR.pr_open, verb = "opened";
    if (b.action === "closed" && pr.merged) { color = COLOR.pr_merged; verb = "merged"; }
    else if (b.action === "closed") { color = COLOR.pr_closed; verb = "closed"; }
    else if (b.action === "reopened") { verb = "reopened"; }
    else if (b.action !== "opened") return;

    return postEmbed(CH_PULL_REQUESTS, {
      title: `🔀 PR ${verb}: ${pr.title}`,
      url: pr.html_url,
      description: trim(pr.body, 800) || "_(no description)_",
      color,
      author: authorOf(pr.user),
      fields: [
        { name: "Branch", value: `\`${pr.head.ref}\` → \`${pr.base.ref}\``, inline: true },
        { name: "Changes", value: `+${pr.additions} / -${pr.deletions} (${pr.changed_files} files)`, inline: true },
      ],
      footer: { text: `${b.repository.full_name} • #${pr.number}` },
      timestamp: pr.updated_at,
    });
  }

  if (event === "push" && b.ref === `refs/heads/${b.repository.default_branch}`) {
    const commits = (b.commits || []).filter(c => !c.message.startsWith("Merge "));
    if (commits.length === 0) return;
    const lines = commits.slice(0, 8).map(c => {
      const sha = c.id.slice(0, 7);
      const msg = c.message.split("\n")[0];
      return `[\`${sha}\`](${c.url}) ${escapeMd(msg)} — ${escapeMd(c.author.name)}`;
    });
    if (commits.length > 8) lines.push(`_…and ${commits.length - 8} more_`);
    return postEmbed(CH_COMMITS, {
      title: `💾 ${commits.length} commit${commits.length > 1 ? "s" : ""} to ${b.repository.default_branch}`,
      url: b.compare,
      description: lines.join("\n"),
      color: COLOR.push,
      author: authorOf(b.sender),
      footer: { text: b.repository.full_name },
      timestamp: new Date().toISOString(),
    });
  }

  if (event === "issues") {
    if (!["opened", "closed", "reopened"].includes(b.action)) return;
    const i = b.issue;
    const color = b.action === "opened" || b.action === "reopened" ? COLOR.issue_open : COLOR.issue_closed;
    return postEmbed(CH_BUG_REPORTS, {
      title: `🐛 Issue ${b.action}: ${i.title}`,
      url: i.html_url,
      description: trim(i.body, 800) || "_(no description)_",
      color,
      author: authorOf(i.user),
      footer: { text: `${b.repository.full_name} • #${i.number}` },
      timestamp: i.updated_at,
    });
  }

  if (event === "star" && b.action === "created") {
    return postEmbed(CH_RELEASES, {
      title: `⭐ New star — ${b.repository.stargazers_count} total`,
      url: b.repository.html_url,
      color: COLOR.star,
      author: authorOf(b.sender),
      footer: { text: b.repository.full_name },
      timestamp: new Date().toISOString(),
    });
  }
}

function authorOf(u) {
  if (!u) return undefined;
  return { name: u.login || u.name, icon_url: u.avatar_url, url: u.html_url };
}

function trim(s, n) {
  if (!s) return s;
  return s.length > n ? s.slice(0, n - 1) + "…" : s;
}

function escapeMd(s) {
  return String(s).replace(/([\\`*_~|])/g, "\\$1");
}

async function postEmbed(channelId, embed) {
  if (!channelId) {
    app.log.warn({ embed }, "no channel configured, skipping");
    return;
  }
  const res = await fetch(`${DISCORD_API}/channels/${channelId}/messages`, {
    method: "POST",
    headers: {
      "Authorization": `Bot ${DISCORD_TOKEN}`,
      "Content-Type": "application/json",
    },
    body: JSON.stringify({ embeds: [embed] }),
  });
  if (!res.ok) {
    const text = await res.text();
    app.log.error({ status: res.status, text }, "discord post failed");
    throw new Error(`discord ${res.status}`);
  }
}

// Capture raw body for HMAC verification.
app.addContentTypeParser("application/json", { parseAs: "buffer" }, (req, body, done) => {
  req.rawBody = body.toString("utf8");
  try {
    done(null, JSON.parse(req.rawBody));
  } catch (e) {
    done(e);
  }
});

app.listen({ host: "0.0.0.0", port: Number(PORT) })
  .then(addr => app.log.info(`listening on ${addr}`))
  .catch(err => { app.log.error(err); process.exit(1); });

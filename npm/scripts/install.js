'use strict';
// postinstall: download prebuilt `puru` Go binary from GitHub Releases.
// Env override:
//   PURU_AI_VERSION=v1.2.3 | 1.2.3  (default: version di package.json)
//   PURU_AI_REPO=owner/repo         (default: purujawa06-bot/PURU-AI)
//   PURU_AI_SKIP_DOWNLOAD=1         (skip, untuk CI/offline)
const fs = require('fs');
const path = require('path');
const https = require('https');
const { mapOS, mapArch, assetName, binaryPath, downloadURL } = require('./binary');

function pkgVersion() {
  const pkg = JSON.parse(fs.readFileSync(path.join(__dirname, '..', 'package.json'), 'utf8'));
  return pkg.version;
}

function resolveTag() {
  const v = (process.env.PURU_AI_VERSION || pkgVersion()).trim();
  return v.startsWith('v') ? v : `v${v}`;
}

function fetch(url, dest, redirects = 5) {
  return new Promise((resolve, reject) => {
    https.get(url, { headers: { 'User-Agent': 'puru-ai-npm-install' } }, (res) => {
      if (res.statusCode >= 300 && res.statusCode < 400 && res.headers.location) {
        if (redirects === 0) return reject(new Error('Too many redirects'));
        res.resume();
        const next = res.headers.location.startsWith('http')
          ? res.headers.location
          : new URL(res.headers.location, url).toString();
        return fetch(next, dest, redirects - 1).then(resolve, reject);
      }
      if (res.statusCode !== 200) {
        res.resume();
        return reject(new Error(`Download failed HTTP ${res.statusCode}: ${url}`));
      }
      const out = fs.createWriteStream(dest, { mode: 0o755 });
      res.pipe(out);
      out.on('finish', () => out.close(() => resolve()));
      out.on('error', reject);
    }).on('error', reject);
  });
}

async function main() {
  if (process.env.PURU_AI_SKIP_DOWNLOAD === '1') {
    console.log('[puru-ai] skip download (PURU_AI_SKIP_DOWNLOAD=1)');
    return;
  }
  const repo = process.env.PURU_AI_REPO || 'purujawa06-bot/PURU-AI';
  const tag = resolveTag();
  const goos = mapOS();
  const goarch = mapArch();
  const url = downloadURL({ repo, tag, goos, goarch });
  const dest = binaryPath();
  fs.mkdirSync(path.dirname(dest), { recursive: true });
  console.log(`[puru-ai] downloading ${assetName(goos, goarch)} ${tag} from ${repo}...`);
  try {
    await fetch(url, dest);
  } catch (err) {
    try { fs.unlinkSync(dest); } catch (_) {}
    console.error(`[puru-ai] FAILED: ${err.message}`);
    console.error(`[puru-ai] Check the release at https://github.com/${repo}/releases/tag/${tag}`);
    console.error(`[puru-ai] Or set PURU_AI_BINARY=/path/to/puru and reinstall.`);
    throw err;
  }
  if (goos !== 'windows') {
    try { fs.chmodSync(dest, 0o755); } catch (_) {}
  }
  console.log(`[puru-ai] OK: ${dest}`);
  console.log('[puru-ai] Next: puru setup  →  puru gateway');
}

if (require.main === module) {
  main().catch((e) => {
    console.error(e.message || e);
    process.exit(1);
  });
}

module.exports = { resolveTag, fetch };

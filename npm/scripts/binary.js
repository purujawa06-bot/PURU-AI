'use strict';
// Shared helper: resolve platform/arch + vendor binary path.
const path = require('path');
const os = require('os');

function mapOS(platform = process.platform) {
  if (platform === 'win32') return 'windows';
  if (platform === 'darwin') return 'darwin';
  // Termux di Android lapor 'linux' — binary linux statik (CGO_ENABLED=0) jalan langsung.
  if (platform === 'linux' || platform === 'android') return 'linux';
  throw new Error(`Unsupported OS: ${platform} (supported: linux, darwin, windows)`);
}

function mapArch(arch = process.arch) {
  if (arch === 'x64') return 'amd64';
  if (arch === 'arm64') return 'arm64';
  // HP Android 32-bit (armv7) — Termux lapor 'arm'.
  if (arch === 'arm') return 'arm';
  throw new Error(`Unsupported arch: ${arch} (supported: x64, arm64, arm)`);
}

function assetName(goos, goarch) {
  const base = `puru-${goos}-${goarch}`;
  return goos === 'windows' ? `${base}.exe` : base;
}

function binaryPath() {
  if (process.env.PURU_AI_BINARY) return process.env.PURU_AI_BINARY;
  const goos = mapOS();
  const goarch = mapArch();
  const bin = assetName(goos, goarch);
  // Vendor dir lives next to this file: scripts/vendor/<bin>
  return path.join(__dirname, 'vendor', bin);
}

function downloadURL({ repo, tag, goos, goarch }) {
  return `https://github.com/${repo}/releases/download/${tag}/${assetName(goos, goarch)}`;
}

function cacheDir() {
  return process.env.PURU_AI_CACHE || path.join(os.homedir(), '.puru', 'bin');
}

module.exports = { mapOS, mapArch, assetName, binaryPath, downloadURL, cacheDir };

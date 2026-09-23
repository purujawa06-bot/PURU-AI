#!/usr/bin/env node
'use strict';
// Thin launcher: forward all args to the downloaded Go binary.
const { spawnSync } = require('child_process');
const fs = require('fs');
const { binaryPath } = require('../scripts/binary');

const bin = binaryPath();
if (!fs.existsSync(bin)) {
  console.error(`[puru-ai] binary not found: ${bin}`);
  console.error('[puru-ai] Run `npm rebuild -g @rikipurpur/puru-ai` or reinstall.');
  console.error('[puru-ai] Alternative: set PURU_AI_BINARY=/path/to/puru');
  process.exit(1);
}
const res = spawnSync(bin, process.argv.slice(2), { stdio: 'inherit' });
process.exit(res.status == null ? 1 : res.status);

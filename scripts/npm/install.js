#!/usr/bin/env node
// npm postinstall: downloads the platform binary, verifies sha256, places it in ./bin/adept.

const fs = require('fs');
const path = require('path');
const https = require('https');
const crypto = require('crypto');
const zlib = require('zlib');
const { spawnSync } = require('child_process');
const tar = require('tar');

const REPO = 'itaywol/adeptability';
const pkg = require('./package.json');
const VERSION = `v${pkg.version}`;

const PLATFORM_MAP = {
  'darwin-x64': 'darwin_amd64',
  'darwin-arm64': 'darwin_arm64',
  'linux-x64': 'linux_amd64',
  'linux-arm64': 'linux_arm64',
  'win32-x64': 'windows_amd64',
};

const key = `${process.platform}-${process.arch}`;
const target = PLATFORM_MAP[key];
if (!target) {
  console.error(`adeptability: unsupported platform ${key}`);
  process.exit(1);
}

const ext = process.platform === 'win32' ? 'zip' : 'tar.gz';
const archive = `adeptability_${pkg.version}_${target}.${ext}`;
const url = `https://github.com/${REPO}/releases/download/${VERSION}/${archive}`;
const sumsURL = `https://github.com/${REPO}/releases/download/${VERSION}/checksums.txt`;

const binDir = path.join(__dirname, 'bin');
fs.mkdirSync(binDir, { recursive: true });

function fetch(u) {
  return new Promise((resolve, reject) => {
    https.get(u, (res) => {
      if (res.statusCode >= 300 && res.statusCode < 400 && res.headers.location) {
        return fetch(res.headers.location).then(resolve, reject);
      }
      if (res.statusCode !== 200) return reject(new Error(`HTTP ${res.statusCode} for ${u}`));
      const chunks = [];
      res.on('data', (c) => chunks.push(c));
      res.on('end', () => resolve(Buffer.concat(chunks)));
      res.on('error', reject);
    }).on('error', reject);
  });
}

(async () => {
  console.log(`adeptability: downloading ${url}`);
  const [body, sums] = await Promise.all([fetch(url), fetch(sumsURL)]);
  const want = sums.toString().split('\n')
    .map((l) => l.trim())
    .find((l) => l.endsWith(archive));
  if (!want) throw new Error(`checksum line missing for ${archive}`);
  const expected = want.split(/\s+/)[0];
  const got = crypto.createHash('sha256').update(body).digest('hex');
  if (got !== expected) throw new Error(`sha256 mismatch: want ${expected} got ${got}`);

  if (ext === 'zip') {
    const AdmZip = require('adm-zip');
    const zip = new AdmZip(body);
    zip.extractAllTo(binDir, true);
  } else {
    const buf = zlib.gunzipSync(body);
    const tmpTar = path.join(binDir, 'archive.tar');
    fs.writeFileSync(tmpTar, buf);
    spawnSync('tar', ['-xf', tmpTar, '-C', binDir], { stdio: 'inherit' });
    fs.unlinkSync(tmpTar);
  }

  const binName = process.platform === 'win32' ? 'adept.exe' : 'adept';
  const dst = path.join(binDir, binName);
  if (!fs.existsSync(dst)) throw new Error(`expected binary at ${dst}`);
  if (process.platform !== 'win32') fs.chmodSync(dst, 0o755);
  console.log(`adeptability: installed ${dst}`);
})().catch((err) => {
  console.error(`adeptability: install failed: ${err.message}`);
  process.exit(1);
});

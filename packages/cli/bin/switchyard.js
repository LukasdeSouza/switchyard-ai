#!/usr/bin/env node

const os = require('os');
const path = require('path');
const fs = require('fs');
const { spawnSync } = require('child_process');

function getBinaryPath() {
  const platform = os.platform();
  const arch = os.arch();

  // 1. Try optional platform-specific npm package (smaller installs)
  const platformPkgMap = {
    'darwin-arm64': ['switchyard-ai-darwin-arm64', 'switchyard'],
    'darwin-x64':  ['switchyard-ai-darwin-arm64', 'switchyard'], // fallback
    'linux-x64':   ['switchyard-ai-linux-x64', 'switchyard'],
    'win32-x64':   ['switchyard-ai-win32-x64', 'switchyard.exe'],
  };
  const key = `${platform}-${arch}`;
  const pkgEntry = platformPkgMap[key];
  if (pkgEntry) {
    try {
      const optBin = require.resolve(`${pkgEntry[0]}/bin/${pkgEntry[1]}`);
      if (fs.existsSync(optBin)) {
        if (platform !== 'win32') {
          try { fs.chmodSync(optBin, 0o755); } catch (_) {}
        }
        return optBin;
      }
    } catch (_) {}
  }

  // 2. Fallback: bundled binary inside this package
  let binaryName = '';
  if (platform === 'win32') {
    binaryName = 'switchyard-windows-amd64.exe';
  } else if (platform === 'darwin') {
    binaryName = 'switchyard-darwin-arm64';
  } else if (platform === 'linux') {
    binaryName = 'switchyard-linux-amd64';
  } else {
    console.error(`[Switchyard] Unsupported platform: ${platform} (${arch})`);
    process.exit(1);
  }

  const binaryPath = path.join(__dirname, binaryName);
  if (!fs.existsSync(binaryPath)) {
    const localFallback = path.join(__dirname, platform === 'win32' ? 'switchyard.exe' : 'switchyard');
    if (fs.existsSync(localFallback)) return localFallback;
    console.error(`[Switchyard] Binary not found for ${platform}-${arch} at ${binaryPath}`);
    process.exit(1);
  }

  if (platform !== 'win32') {
    try { fs.chmodSync(binaryPath, 0o755); } catch (_) {}
  }
  return binaryPath;
}

function run() {
  const binary = getBinaryPath();
  const args = process.argv.slice(2);
  const result = spawnSync(binary, args, {
    stdio: 'inherit',
    windowsHide: false,
  });
  if (result.error) {
    console.error(`[Switchyard] Failed to execute process: ${result.error.message}`);
    process.exit(1);
  }
  process.exit(result.status ?? 0);
}

run();

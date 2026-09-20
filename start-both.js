const { spawn } = require('child_process');

// Start Rate Limiter on default port 8080
console.log('Starting Rate Limiter on 8080...');
const rl = spawn('go', ['run', './cmd/rate-limiter'], {
  cwd: 'C:\\Users\\zoro\\Desktop\\30\\Rate Limiter',
  stdio: 'inherit',
  detached: false,
});

rl.on('error', (err) => console.error('RL error:', err));

// Wait for Rate Limiter to start
setTimeout(() => {
  console.log('Starting URL Shortener on 8080...');
  const us = spawn('node', ['server.js'], {
    cwd: 'C:\\Users\\zoro\\Desktop\\30\\URL-Shorterner',
    stdio: 'inherit',
    detached: false,
  });

  us.on('error', (err) => console.error('US error:', err));
  us.on('exit', (code) => {
    console.log('URL Shortener exited:', code);
    rl.kill();
  });
}, 5000);

rl.on('exit', (code) => {
  console.log('Rate Limiter exited:', code);
});

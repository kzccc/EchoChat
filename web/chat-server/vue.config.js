// 该脚本文件承载当前模块的初始化或辅助逻辑。
const { defineConfig } = require('@vue/cli-service')
const fs = require('fs')
const path = require('path')

const devCertPath = process.env.ECHOCHAT_DEV_CERT || '/etc/ssl/certs/server.crt'
const devKeyPath = process.env.ECHOCHAT_DEV_KEY || '/etc/ssl/private/server.key'
const hasDevHttpsCert = fs.existsSync(devCertPath) && fs.existsSync(devKeyPath)

module.exports = defineConfig({
  transpileDependencies: true,
  lintOnSave: false,
  devServer: {
    host: '0.0.0.0',
    ...(hasDevHttpsCert ? {
      https: {
        cert: fs.readFileSync(path.join(devCertPath)),
        key: fs.readFileSync(path.join(devKeyPath)),
      },
      port: 443,
    } : {
      port: 8080,
    }),
  }
})

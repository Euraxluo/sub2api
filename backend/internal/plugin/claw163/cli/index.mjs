#!/usr/bin/env node

import { mkdirSync, readFileSync, renameSync, writeFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { homedir } from 'node:os'
import { MailClient } from '@clawemail/node-sdk'
import nodemailer from 'nodemailer'

const VERSION = '0.1.0 (clawemail-sdk 0.2.4)'

function usage() {
  return `Usage: claw163-cli [options] <command>

Options:
  --config <path>   use a custom config path
  --profile <name>  use a named profile
  --json            output JSON
  -v, --version     print version

Commands:
  auth apikey set <key>
  auth login --user <email> [SMTP options]
  auth test
  compose send --to <emails> --subject <subject> --body <body>`
}

function parseGlobalArgs(argv) {
  const options = {
    config: process.env.CLAW163_CONFIG || join(homedir(), '.config', 'claw163-cli', 'config.json'),
    profile: 'default',
  }
  let index = 0
  while (index < argv.length) {
    const arg = argv[index]
    if (arg === '--config' || arg === '--profile') {
      const value = argv[index + 1]
      if (!value) throw new Error(`${arg} requires a value`)
      options[arg.slice(2)] = value
      index += 2
      continue
    }
    if (arg === '--json') {
      index += 1
      continue
    }
    break
  }
  return { options, command: argv.slice(index) }
}

function flag(args, name, fallback = '') {
  const index = args.indexOf(name)
  if (index < 0) return fallback
  const value = args[index + 1]
  if (!value) throw new Error(`${name} requires a value`)
  return value
}

function loadConfig(path) {
  try {
    const parsed = JSON.parse(readFileSync(path, 'utf8'))
    return {
      apiKey: typeof parsed.apiKey === 'string' ? parsed.apiKey : '',
      profiles: parsed.profiles && typeof parsed.profiles === 'object' ? parsed.profiles : {},
    }
  } catch (error) {
    if (error?.code === 'ENOENT') return { apiKey: '', profiles: {} }
    throw new Error('cannot read Claw163 CLI configuration')
  }
}

function saveConfig(path, config) {
  mkdirSync(dirname(path), { recursive: true, mode: 0o700 })
  const temporary = `${path}.tmp`
  writeFileSync(temporary, `${JSON.stringify(config, null, 2)}\n`, { mode: 0o600 })
  renameSync(temporary, path)
}

function selectedAccount(config, profile) {
  const account = config.profiles[profile]
  if (!account?.user) throw new Error(`profile ${profile} is not initialized`)
  return account
}

function sdkClient(config, account) {
  if (!config.apiKey) throw new Error('Claw163 API key is not configured')
  return new MailClient({ apiKey: config.apiKey, user: account.user, logger: null })
}

function smtpClient(account) {
  if (!account.password) throw new Error('Claw163 SMTP credential is not configured')
  return nodemailer.createTransport({
    host: account.smtpHost || 'smtp.claw.163.com',
    port: Number(account.smtpPort || 465),
    secure: Number(account.smtpPort || 465) === 465,
    auth: { user: account.user, pass: account.password },
    connectionTimeout: 15000,
    greetingTimeout: 15000,
    socketTimeout: 30000,
  })
}

function success(data = {}) {
  process.stdout.write(`${JSON.stringify({ success: true, data })}\n`)
}

async function run(argv) {
  if (argv.length === 1 && (argv[0] === '--version' || argv[0] === '-v')) {
    process.stdout.write(`${VERSION}\n`)
    return
  }
  if (argv.length === 0 || (argv.length === 1 && (argv[0] === '--help' || argv[0] === '-h'))) {
    process.stdout.write(`${usage()}\n`)
    return
  }

  const { options, command } = parseGlobalArgs(argv)
  const config = loadConfig(options.config)

  if (command[0] === 'auth' && command[1] === 'apikey' && command[2] === 'set') {
    const apiKey = command[3]
    if (!apiKey) throw new Error('auth apikey set requires a key')
    config.apiKey = apiKey
    saveConfig(options.config, config)
    success()
    return
  }

  if (command[0] === 'auth' && command[1] === 'login') {
    const user = flag(command, '--user')
    if (!user) throw new Error('auth login requires --user')
    const password = flag(command, '--password')
    if (!password && !config.apiKey) throw new Error('auth login requires an API key or SMTP password')
    config.profiles[options.profile] = password
      ? {
          user,
          transport: 'smtp',
          password,
          smtpHost: flag(command, '--smtp-host', 'smtp.claw.163.com'),
          smtpPort: Number(flag(command, '--smtp-port', '465')),
        }
      : { user, transport: 'sdk' }
    saveConfig(options.config, config)
    success({ profile: options.profile, user })
    return
  }

  if (command[0] === 'auth' && command[1] === 'test') {
    const account = selectedAccount(config, options.profile)
    if (account.transport === 'smtp') await smtpClient(account).verify()
    else await sdkClient(config, account).getAccessToken()
    success({ profile: options.profile, user: account.user })
    return
  }

  if (command[0] === 'compose' && command[1] === 'send') {
    const account = selectedAccount(config, options.profile)
    const recipients = flag(command, '--to').split(',').map(value => value.trim()).filter(Boolean)
    const subject = flag(command, '--subject')
    const body = flag(command, '--body')
    if (recipients.length === 0) throw new Error('compose send requires --to')
    if (account.transport === 'smtp') {
      await smtpClient(account).sendMail({ from: account.user, to: recipients, subject, text: body })
    } else {
      await sdkClient(config, account).mail.send({ to: recipients, subject, body })
    }
    success({ status: 'sent' })
    return
  }

  throw new Error('unsupported claw163-cli command')
}

run(process.argv.slice(2)).catch(error => {
  const message = error instanceof Error ? error.message : String(error)
  process.stderr.write(`${JSON.stringify({ success: false, error: { message } })}\n`)
  process.exitCode = 1
})

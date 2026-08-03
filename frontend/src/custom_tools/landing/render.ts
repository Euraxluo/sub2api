import templateHtml from './index.html?raw'
import { LANDING_DEFAULTS } from './params'

const TEXT_MAP: Array<[string, string]> = [
  ['siteName', 'site_name'],
  ['navSiteName', 'site_name'],
  ['siteSubtitle', 'site_subtitle'],
  ['eyebrowText', 'eyebrow'],
  ['lede', 'lede'],
  ['ctaPrimaryText', 'cta_primary'],
  ['ctaSecondary', 'cta_secondary'],
  ['loginText', 'login_text'],
  ['tag1', 'tag1'],
  ['tag2', 'tag2'],
  ['tag3', 'tag3'],
  ['bubbleUser', 'bubble_user'],
  ['statusText', 'status'],
  ['painTitle', 'pain_title'],
  ['painDesc', 'pain_desc'],
  ['featuresTitle', 'features_title'],
  ['featuresDesc', 'features_desc'],
  ['compareTitle', 'compare_title'],
  ['compareDesc', 'compare_desc'],
  ['providersTitle', 'providers_title'],
  ['providersDesc', 'providers_desc'],
  ['ctaTitle', 'cta_title'],
  ['ctaDesc', 'cta_desc'],
  ['ctaButton', 'cta_button'],
  ['footerNote', 'footer_note'],
]

function pick(cfg: Record<string, string>, key: string): string {
  return Object.prototype.hasOwnProperty.call(cfg, key) ? cfg[key] : LANDING_DEFAULTS[key]
}

function normalizeAccent(raw: string): string {
  const value = String(raw || '').trim()
  if (/^#[0-9a-fA-F]{6}$/.test(value)) return value
  if (/^#[0-9a-fA-F]{3}$/.test(value)) {
    return `#${value
      .slice(1)
      .split('')
      .map((c) => c + c)
      .join('')}`
  }
  return ''
}

function applyConfig(doc: Document, cfg: Record<string, string>, hidden: string[]) {
  const name = pick(cfg, 'site_name').trim() || LANDING_DEFAULTS.site_name

  doc.title = name
  const brandInitial = doc.getElementById('brandInitial')
  if (brandInitial) brandInitial.textContent = name.charAt(0).toUpperCase()
  const copyright = doc.getElementById('copyright')
  if (copyright) {
    copyright.textContent = `© ${new Date().getFullYear()} ${name} · 保留所有权利`
  }

  for (const [id, key] of TEXT_MAP) {
    const el = doc.getElementById(id)
    if (!el) continue
    el.textContent = key === 'site_name' ? name : pick(cfg, key)
  }

  const bubbleAi = doc.getElementById('bubbleAi')
  if (bubbleAi) bubbleAi.innerHTML = pick(cfg, 'bubble_ai')

  const logo = pick(cfg, 'site_logo').trim()
  if (logo) {
    const mark = doc.getElementById('brandMark')
    const img = doc.getElementById('siteLogo') as HTMLImageElement | null
    if (mark && img) {
      img.src = logo
      img.alt = name
      mark.classList.add('has-logo')
    }
  }

  const docUrl = pick(cfg, 'doc_url').trim()
  if (docUrl) {
    const link = doc.getElementById('docLink') as HTMLAnchorElement | null
    if (link) {
      link.href = docUrl
      link.hidden = false
    }
  }

  for (const id of ['tag1', 'tag2', 'tag3']) {
    const el = doc.getElementById(id)
    if (el && !String(el.textContent || '').trim()) {
      (el as HTMLElement).style.display = 'none'
    }
  }

  const accent = normalizeAccent(pick(cfg, 'accent'))
  if (accent) {
    const root = doc.documentElement
    root.style.setProperty('--accent', accent)
    root.style.setProperty('--accent-deep', accent)
    root.style.setProperty('--accent-soft', `${accent}1f`)
    root.style.setProperty('--glow', `${accent}2e`)
  }

  for (const id of hidden) {
    const el = doc.getElementById(id)
    if (el) (el as HTMLElement).style.display = 'none'
  }

  // home_content / srcdoc 不执行模板里的 boot 脚本；渲染结果已是最终态。
  doc.querySelectorAll('script').forEach((node) => node.remove())
}

function parseTemplate(): Document {
  return new DOMParser().parseFromString(templateHtml, 'text/html')
}

/** Full HTML document for Admin Tools iframe[srcdoc] preview. */
export function renderLandingDocument(params: Record<string, string>, hidden: string[] = []): string {
  const doc = parseTemplate()
  applyConfig(doc, params, hidden)
  return `<!DOCTYPE html>\n${doc.documentElement.outerHTML}`
}

/**
 * HTML fragment for site setting home_content (v-html mode).
 * Avoids same-origin iframe / CSP framing requirements.
 */
export function renderLandingFragment(params: Record<string, string>, hidden: string[] = []): string {
  const doc = parseTemplate()
  applyConfig(doc, params, hidden)

  const styles = Array.from(doc.querySelectorAll('style'))
    .map((node) => node.outerHTML)
    .join('\n')
  const page = doc.querySelector('.page')
  const accent = normalizeAccent(pick(params, 'accent'))
  const styleAttr = accent
    ? ` style="--accent:${accent};--accent-deep:${accent};--accent-soft:${accent}1f;--glow:${accent}2e;"`
    : ''

  return [
    '<!-- custom_tools landing: baked HTML for home_content -->',
    styles,
    `<div class="custom-landing-root"${styleAttr}>`,
    page ? page.outerHTML : doc.body.innerHTML,
    '</div>',
  ].join('\n')
}

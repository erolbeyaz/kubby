import { describe, expect, it } from 'vitest'

import { TEMPLATES, groupedTemplates, renderTemplate } from './templates'

const byKind = (kind: string) => {
  const template = TEMPLATES.find((entry) => entry.kind === kind)
  if (!template) throw new Error(`no template for ${kind}`)
  return template
}

describe('templates', () => {
  it('fills the namespace when exactly one is in scope', () => {
    expect(renderTemplate(byKind('Deployment'), ['payments'])).toContain('namespace: payments')
  })

  // Guessing would create the object somewhere the person did not choose, which is the
  // one mistake a template must not make.
  it('leaves the namespace blank when several are in scope', () => {
    const rendered = renderTemplate(byKind('Deployment'), ['payments', 'storefront'])

    expect(rendered).toContain('namespace:')
    expect(rendered).not.toContain('namespace: payments')
  })

  it('leaves the namespace blank when none is chosen', () => {
    expect(renderTemplate(byKind('Deployment'), [])).toMatch(/^ {2}namespace:\s*$/m)
  })

  // A metadata.namespace on a cluster-scoped kind is rejected by the API server. A
  // namespace deeper in the manifest — a RoleBinding subject, say — is a different
  // field and perfectly valid.
  it('gives cluster-scoped kinds no metadata namespace', () => {
    for (const template of TEMPLATES.filter((entry) => !entry.namespaced)) {
      expect(template.manifest).not.toContain('{{namespace}}')
      expect(template.manifest).not.toMatch(/^ {2}namespace:/m)
    }
  })

  it('every template names its own kind and apiVersion', () => {
    for (const template of TEMPLATES) {
      expect(template.manifest).toContain(`kind: ${template.kind}`)
      expect(template.manifest).toMatch(/^apiVersion: \S+/m)
    }
  })

  it('sorts alphabetically within each group, ungrouped first', () => {
    const groups = groupedTemplates(null)

    expect(groups[0]?.group).toBe('')
    for (const { items } of groups) {
      const kinds = items.map((item) => item.kind)
      expect(kinds).toEqual([...kinds].sort((a, b) => a.localeCompare(b)))
    }
  })

  // A cluster without Gateway API would otherwise offer a manifest it cannot accept
  // (ADR-046).
  it('hides kinds the cluster does not serve', () => {
    const groups = groupedTemplates(new Set(['Deployment', 'Service']))
    const kinds = groups.flatMap(({ items }) => items.map((item) => item.kind))

    expect(kinds).toEqual(['Deployment', 'Service'])
  })
})

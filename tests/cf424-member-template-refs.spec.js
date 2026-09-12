// tests/cf424-member-template-refs.spec.js
import { test, expect } from '@playwright/test';
import { fanOut, isRawParamRef } from '../web-proto/js/wires.js';
import { cleanMemberRefs, renameMemberRefs } from '../web-proto/js/regions/inspector/xrd.js';

test.describe('CF-424 — Renaming or deleting XRD parameter member properties ignores Go template index and hasKey expressions', () => {
  test('fanOut, cleanMemberRefs, and renameMemberRefs support index and hasKey member expressions', () => {
    const doc = {
      spec: {
        xrd: {
          parameters: {
            cfg: {
              type: 'object',
              properties: { region: { type: 'string' } }
            }
          }
        },
        templates: {
          queueEndpoint: '{{ index $spec.cfg "region" }}',
          hasKeyGuard: '{{ if hasKey $spec.cfg "region" }}active{{ end }}'
        },
        resources: [{
          name: 'queue',
          fields: {
            regionField: { raw: '{{ index $spec.cfg "region" }}' },
            guardField: { raw: '{{ if hasKey $spec.cfg "region" }}ok{{ end }}' }
          }
        }]
      }
    };

    // 1. isRawParamRef detects index and hasKey on object parameter members
    expect(isRawParamRef(doc.spec.templates.queueEndpoint, 'cfg.region')).toBe(true);
    expect(isRawParamRef(doc.spec.templates.hasKeyGuard, 'cfg.region')).toBe(true);

    // 2. FanOut returns active reference count
    expect(fanOut(doc, 'cfg.region')).toBeGreaterThan(0);

    // 3. Renaming member rewrites index and hasKey expressions
    const draftRename = JSON.parse(JSON.stringify(doc));
    renameMemberRefs(draftRename, 'cfg', 'region', 'regionName');
    expect(draftRename.spec.templates.queueEndpoint).toBe('{{ index $spec.cfg "regionName" }}');
    expect(draftRename.spec.templates.hasKeyGuard).toBe('{{ if hasKey $spec.cfg "regionName" }}active{{ end }}');

    // 4. Deleting member cleans templates and fields
    const draftDelete = JSON.parse(JSON.stringify(doc));
    cleanMemberRefs(draftDelete, 'cfg', 'region');
    expect(draftDelete.spec.templates.queueEndpoint).toBeUndefined();
    expect(draftDelete.spec.templates.hasKeyGuard).toBeUndefined();
  });

  test('supports single quotes, backticks, and quoted spec index/hasKey syntax', () => {
    const doc = {
      spec: {
        xrd: {
          parameters: {
            cfg: {
              type: 'object',
              properties: { region: { type: 'string' } }
            }
          }
        },
        templates: {
          singleQuote: "{{ index $spec.cfg 'region' }}",
          backtick: '{{ index $spec.cfg `region` }}',
          quotedSpec: '{{ index .spec "cfg" "region" }}',
          hasKeyQuotedSpec: '{{ if hasKey .spec "cfg" "region" }}yes{{ end }}'
        },
        resources: [{
          name: 'worker',
          fields: {
            f1: { raw: "{{ index $spec.cfg 'region' }}" },
            f2: { raw: '{{ index .spec "cfg" "region" }}' }
          },
          envelope: {
            e1: { raw: '{{ if hasKey $spec.cfg "region" }}env-val{{ end }}' }
          },
          annotations: {
            a1: { raw: '{{ index $spec.cfg `region` }}' }
          }
        }]
      }
    };

    expect(isRawParamRef(doc.spec.templates.singleQuote, 'cfg.region')).toBe(true);
    expect(isRawParamRef(doc.spec.templates.backtick, 'cfg.region')).toBe(true);
    expect(isRawParamRef(doc.spec.templates.quotedSpec, 'cfg.region')).toBe(true);
    expect(isRawParamRef(doc.spec.templates.hasKeyQuotedSpec, 'cfg.region')).toBe(true);

    // 4 templates + 2 fields + 1 envelope + 1 annotation = 8 references
    expect(fanOut(doc, 'cfg.region')).toBe(8);
    expect(fanOut(doc, 'cfg')).toBe(8);

    const draftRename = JSON.parse(JSON.stringify(doc));
    renameMemberRefs(draftRename, 'cfg', 'region', 'regionName');
    expect(draftRename.spec.templates.singleQuote).toBe("{{ index $spec.cfg 'regionName' }}");
    expect(draftRename.spec.templates.backtick).toBe('{{ index $spec.cfg `regionName` }}');
    expect(draftRename.spec.templates.quotedSpec).toBe('{{ index .spec "cfg" "regionName" }}');
    expect(draftRename.spec.templates.hasKeyQuotedSpec).toBe('{{ if hasKey .spec "cfg" "regionName" }}yes{{ end }}');
    expect(draftRename.spec.resources[0].fields.f1.raw).toBe("{{ index $spec.cfg 'regionName' }}");
    expect(draftRename.spec.resources[0].fields.f2.raw).toBe('{{ index .spec "cfg" "regionName" }}');
    expect(draftRename.spec.resources[0].envelope.e1.raw).toBe('{{ if hasKey $spec.cfg "regionName" }}env-val{{ end }}');
    expect(draftRename.spec.resources[0].annotations.a1.raw).toBe('{{ index $spec.cfg `regionName` }}');

    const draftDelete = JSON.parse(JSON.stringify(doc));
    cleanMemberRefs(draftDelete, 'cfg', 'region');
    expect(draftDelete.spec.templates.singleQuote).toBeUndefined();
    expect(draftDelete.spec.templates.backtick).toBeUndefined();
    expect(draftDelete.spec.templates.quotedSpec).toBeUndefined();
    expect(draftDelete.spec.templates.hasKeyQuotedSpec).toBeUndefined();
    expect(draftDelete.spec.resources[0].fields.f1).toBeUndefined();
    expect(draftDelete.spec.resources[0].fields.f2).toBeUndefined();
    expect(draftDelete.spec.resources[0].envelope).toBeUndefined();
    expect(draftDelete.spec.resources[0].annotations).toBeUndefined();
  });

  test('dotted references like $spec.cfg.region continue to work with fanOut, rename and delete', () => {
    const doc = {
      spec: {
        xrd: {
          parameters: {
            cfg: {
              type: 'object',
              properties: { region: { type: 'string' } }
            }
          }
        },
        templates: {
          tDotted: '{{ $spec.cfg.region }}'
        },
        resources: [{
          name: 'r1',
          fields: {
            fDotted: { raw: 'prefix-{{ .spec.cfg.region }}-suffix' }
          }
        }]
      }
    };

    expect(isRawParamRef(doc.spec.templates.tDotted, 'cfg.region')).toBe(true);
    expect(isRawParamRef(doc.spec.resources[0].fields.fDotted.raw, 'cfg.region')).toBe(true);
    expect(fanOut(doc, 'cfg.region')).toBe(2);

    const draftRename = JSON.parse(JSON.stringify(doc));
    renameMemberRefs(draftRename, 'cfg', 'region', 'regionName');
    expect(draftRename.spec.templates.tDotted).toBe('{{ $spec.cfg.regionName }}');
    expect(draftRename.spec.resources[0].fields.fDotted.raw).toBe('prefix-{{ .spec.cfg.regionName }}-suffix');

    const draftDelete = JSON.parse(JSON.stringify(doc));
    cleanMemberRefs(draftDelete, 'cfg', 'region');
    expect(draftDelete.spec.templates.tDotted).toBeUndefined();
    expect(draftDelete.spec.resources[0].fields.fDotted).toBeUndefined();
  });
});

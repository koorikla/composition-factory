const { test, expect } = require('@playwright/test');
const { resetDoc, guardPageErrors, canvasSettled } = require('./helpers');

test.describe('CF-425 — renaming or deleting environment keys handles Go template hasKey expressions', () => {
  guardPageErrors();

  test.beforeEach(async ({ request }) => {
    await resetDoc(request);
  });

  test('isRawEnvRef matches Go template hasKey expressions checking environment context', async ({ page }) => {
    await page.goto('/');
    await canvasSettled(page);

    const results = await page.evaluate(async () => {
      const { isRawEnvRef } = await import('/js/utils.js');

      return {
        hasKeyDollar: isRawEnvRef('{{ if hasKey $env "region" }}active{{ end }}', 'region'),
        hasKeyDot: isRawEnvRef('{{ if hasKey .env "region" }}active{{ end }}', 'region'),
        hasKeyBare: isRawEnvRef('{{ if hasKey env "region" }}active{{ end }}', 'region'),
        hasKeyDollarDot: isRawEnvRef('{{ if hasKey $.env "region" }}active{{ end }}', 'region'),
        hasKeySingleQuote: isRawEnvRef("{{ if hasKey $env 'region' }}active{{ end }}", 'region'),
        hasKeyBacktick: isRawEnvRef('{{ if hasKey $env `region` }}active{{ end }}', 'region'),
        hasKeyAnd: isRawEnvRef('{{ if and (hasKey $env "region") (eq .region "us-east-1") }}active{{ end }}', 'region'),
        hasKeyRawWhen: isRawEnvRef('hasKey $env "region"', 'region'),
        // Negative checks
        hasKeyOtherKey: isRawEnvRef('{{ if hasKey $env "zone" }}active{{ end }}', 'region'),
        hasKeySpecNotEnv: isRawEnvRef('{{ if hasKey $spec "region" }}active{{ end }}', 'region'),
        hasKeySubstrMismatch: isRawEnvRef('{{ if hasKey $env "regional" }}active{{ end }}', 'region'),
        hasKeyParamsNotEnv: isRawEnvRef('{{ if hasKey $params "region" }}active{{ end }}', 'region'),
      };
    });

    expect(results.hasKeyDollar).toBe(true);
    expect(results.hasKeyDot).toBe(true);
    expect(results.hasKeyBare).toBe(true);
    expect(results.hasKeyDollarDot).toBe(true);
    expect(results.hasKeySingleQuote).toBe(true);
    expect(results.hasKeyBacktick).toBe(true);
    expect(results.hasKeyAnd).toBe(true);
    expect(results.hasKeyRawWhen).toBe(true);

    expect(results.hasKeyOtherKey).toBe(false);
    expect(results.hasKeySpecNotEnv).toBe(false);
    expect(results.hasKeySubstrMismatch).toBe(false);
    expect(results.hasKeyParamsNotEnv).toBe(false);
  });

  test('replaceRawEnv and renameEnvKeyInDoc rename environment key references in hasKey expressions', async ({ page }) => {
    await page.goto('/');
    await canvasSettled(page);

    const result = await page.evaluate(async () => {
      const { replaceRawEnv, renameEnvKeyInDoc } = await import('/js/utils.js');

      const tmplHasKey = '{{ if hasKey $env "region" }}active{{ end }}';
      const tmplHasKeyAnd = '{{ if and (hasKey $env "region") (eq $env.region "us-east-1") }}active{{ end }}';
      const tmplHasKeyDot = '{{ if hasKey .env "region" }}active{{ end }}';
      const tmplHasKeyBareSingle = "{{ if hasKey env 'region' }}active{{ end }}";
      const tmplHasKeyBacktick = '{{ if hasKey $env `region` }}active{{ end }}';

      const replacedSimple = replaceRawEnv(tmplHasKey, 'region', 'location');
      const replacedAnd = replaceRawEnv(tmplHasKeyAnd, 'region', 'location');
      const replacedDot = replaceRawEnv(tmplHasKeyDot, 'region', 'location');
      const replacedBareSingle = replaceRawEnv(tmplHasKeyBareSingle, 'region', 'location');
      const replacedBacktick = replaceRawEnv(tmplHasKeyBacktick, 'region', 'location');

      const draft = {
        apiVersion: 'factory.crossplane.io/v1alpha1',
        kind: 'Blueprint',
        metadata: { name: 'test-env-has-key-rename' },
        spec: {
          environment: {
            region: { type: 'string' },
            other: { type: 'string' }
          },
          templates: {
            regionGuard: tmplHasKey,
            otherGuard: '{{ if hasKey $env "other" }}enabled{{ end }}'
          },
          resources: [
            {
              name: 'bucket',
              type: 's3.aws.upbound.io/v1beta1',
              fields: {
                f: { raw: tmplHasKey },
                keep: { raw: '{{ if hasKey $env "other" }}ok{{ end }}' }
              },
              when: 'hasKey $env "region"'
            }
          ]
        }
      };

      renameEnvKeyInDoc(draft, 'region', 'location');

      return {
        replacedSimple,
        replacedAnd,
        replacedDot,
        replacedBareSingle,
        replacedBacktick,
        draft
      };
    });

    expect(result.replacedSimple).toBe('{{ if hasKey $env "location" }}active{{ end }}');
    expect(result.replacedAnd).toBe('{{ if and (hasKey $env "location") (eq $env.location "us-east-1") }}active{{ end }}');
    expect(result.replacedDot).toBe('{{ if hasKey .env "location" }}active{{ end }}');
    expect(result.replacedBareSingle).toBe("{{ if hasKey env 'location' }}active{{ end }}");
    expect(result.replacedBacktick).toBe('{{ if hasKey $env `location` }}active{{ end }}');

    expect(result.draft.spec.environment.location).toBeDefined();
    expect(result.draft.spec.environment.region).toBeUndefined();
    expect(result.draft.spec.templates.regionGuard).toBe('{{ if hasKey $env "location" }}active{{ end }}');
    expect(result.draft.spec.templates.otherGuard).toBe('{{ if hasKey $env "other" }}enabled{{ end }}');
    expect(result.draft.spec.resources[0].fields.f.raw).toBe('{{ if hasKey $env "location" }}active{{ end }}');
    expect(result.draft.spec.resources[0].fields.keep.raw).toBe('{{ if hasKey $env "other" }}ok{{ end }}');
    expect(result.draft.spec.resources[0].when).toBe('hasKey $env "location"');
  });

  test('cleanEnvRefs and deleteEnvKeyFromDoc remove templates, fields, and when conditions containing hasKey', async ({ page }) => {
    await page.goto('/');
    await canvasSettled(page);

    const result = await page.evaluate(async () => {
      const { deleteEnvKeyFromDoc } = await import('/js/utils.js');

      const tmplHasKey = '{{ if hasKey $env "region" }}active{{ end }}';
      const tmplOther = '{{ if hasKey $env "other" }}active{{ end }}';

      const draft = {
        apiVersion: 'factory.crossplane.io/v1alpha1',
        kind: 'Blueprint',
        metadata: { name: 'test-env-has-key-clean' },
        spec: {
          environment: {
            region: { type: 'string' },
            other: { type: 'string' }
          },
          templates: {
            regionGuard: tmplHasKey,
            otherGuard: tmplOther
          },
          resources: [
            {
              name: 'bucket',
              type: 's3.aws.upbound.io/v1beta1',
              fields: {
                f: { raw: tmplHasKey },
                keep: { raw: tmplOther }
              },
              when: 'hasKey $env "region"'
            }
          ]
        }
      };

      deleteEnvKeyFromDoc(draft, 'region');
      return draft;
    });

    expect(result.spec.templates.regionGuard).toBeUndefined();
    expect(result.spec.templates.otherGuard).toBe('{{ if hasKey $env "other" }}active{{ end }}');

    const bucket = result.spec.resources[0];
    expect(bucket.fields.f).toBeUndefined();
    expect(bucket.fields.keep).toEqual({ raw: '{{ if hasKey $env "other" }}active{{ end }}' });
    expect(bucket.when).toBeUndefined();
    expect(result.spec.environment.region).toBeUndefined();
    expect(result.spec.environment.other).toBeDefined();
  });

  test('findEnvWires and envFanOut identify templates, raw fields, and when guards referencing environment keys via hasKey', async ({ page }) => {
    await page.goto('/');
    await canvasSettled(page);

    const result = await page.evaluate(async () => {
      const { findEnvWires, envFanOut } = await import('/js/wires.js');

      const tmplHasKey = '{{ if hasKey $env "region" }}active{{ end }}';

      const doc = {
        apiVersion: 'factory.crossplane.io/v1alpha1',
        kind: 'Blueprint',
        metadata: { name: 'test-wires-has-key' },
        spec: {
          environment: {
            region: { type: 'string' },
            unused: { type: 'string' }
          },
          templates: {
            regionGuard: tmplHasKey
          },
          resources: [
            {
              name: 'bucket',
              type: 's3.aws.upbound.io/v1beta1',
              fields: {
                f: { raw: tmplHasKey }
              },
              when: 'hasKey $env "region"'
            }
          ]
        }
      };

      return {
        wiresRegion: findEnvWires(doc, 'region'),
        fanOutRegion: envFanOut(doc, 'region'),
        wiresUnused: findEnvWires(doc, 'unused'),
        fanOutUnused: envFanOut(doc, 'unused')
      };
    });

    expect(result.wiresRegion).toContain('templates.regionGuard');
    expect(result.wiresRegion).toContain('bucket.f');
    expect(result.wiresRegion).toContain('bucket.when');
    expect(result.wiresRegion.length).toBe(3);
    expect(result.fanOutRegion).toBe(3);

    expect(result.wiresUnused).toEqual([]);
    expect(result.fanOutUnused).toBe(0);
  });
});

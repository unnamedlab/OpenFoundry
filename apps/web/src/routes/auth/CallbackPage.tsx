import { useEffect, useState } from 'react';
import { Link, useLocation, useNavigate } from 'react-router-dom';

import {
  clearStoredAuthReturnTo,
  getAuthReturnTo,
  getStoredAuthReturnTo,
  rememberAuthReturnTo,
  withAuthReturnTo,
} from '@/lib/auth/redirects';
import { auth } from '@stores/auth';
import { useTranslator } from '@/lib/i18n/store';

type CallbackResult = Awaited<ReturnType<typeof auth.handleSsoCallback>>;
type CallbackStep = 0 | 1 | 2;

const callbackCompletions = new Map<string, Promise<CallbackResult>>();
const SENSITIVE_CALLBACK_PARAMS = ['access_token', 'refresh_token', 'token_type', 'expires_in'];

function paramsFromHash(hash: string) {
  return new URLSearchParams(hash.startsWith('#') ? hash.slice(1) : hash);
}

// hasTokenFragment reports whether the IdP redirected with a
// `#access_token=…` style fragment (legacy implicit-flow shape). With
// httpOnly-cookie auth the SPA never reads the token; we still detect
// the shape so the callback path can fall through to /users/me — the
// cookie has already been set by the time the browser lands here.
function hasTokenFragment(values: URLSearchParams) {
  return values.get('access_token') !== null;
}

function completeCallbackOnce(key: string, run: () => Promise<CallbackResult>) {
  const existing = callbackCompletions.get(key);
  if (existing) return existing;

  const next = run().finally(() => {
    callbackCompletions.delete(key);
  });
  callbackCompletions.set(key, next);
  return next;
}

function removeSensitiveCallbackParams(pathname: string, search: string) {
  const params = new URLSearchParams(search);
  for (const key of SENSITIVE_CALLBACK_PARAMS) params.delete(key);
  const qs = params.toString();
  return `${pathname}${qs ? `?${qs}` : ''}`;
}

export function CallbackPage() {
  const t = useTranslator();
  const navigate = useNavigate();
  const location = useLocation();
  const [error, setError] = useState('');
  const [step, setStep] = useState<CallbackStep>(0);
  const intendedReturnTo = getAuthReturnTo(location.search) ?? getStoredAuthReturnTo();
  const postAuthRedirect = intendedReturnTo ?? '/';

  useEffect(() => {
    document.title = t('auth.callback.title');
  }, [t]);

  useEffect(() => {
    const query = new URLSearchParams(location.search);
    const fragment = paramsFromHash(location.hash);
    const providerError = fragment.get('error') ?? query.get('error');
    const providerErrorDescription = fragment.get('error_description') ?? query.get('error_description');

    setError('');
    setStep(0);

    if (providerError) {
      setError(t('auth.callback.providerError', { message: providerErrorDescription || providerError }));
      return;
    }

    let cancelled = false;
    (async () => {
      try {
        const tokenInFragment = hasTokenFragment(fragment) || hasTokenFragment(query);
        const callbackKey = tokenInFragment
          ? `token:${removeSensitiveCallbackParams(location.pathname, location.search)}`
          : `exchange:${location.pathname}${location.search}${location.hash}`;
        let result: CallbackResult;

        if (tokenInFragment) {
          setStep(1);
          result = await completeCallbackOnce(callbackKey, () => auth.completeTokenCallback());
          if (!cancelled && typeof window !== 'undefined') {
            window.history.replaceState(
              window.history.state,
              document.title,
              removeSensitiveCallbackParams(location.pathname, location.search),
            );
          }
        } else {
          const code = query.get('code');
          const state = query.get('state');
          const samlResponse = query.get('SAMLResponse');
          const relayState = query.get('RelayState');

          if ((!code || !state) && (!samlResponse || !relayState)) {
            setError(t('auth.callback.missing'));
            return;
          }

          setStep(1);
          result = await completeCallbackOnce(callbackKey, () =>
            auth.handleSsoCallback({
              code: code ?? undefined,
              state: state ?? undefined,
              saml_response: samlResponse ?? undefined,
              relay_state: relayState ?? undefined,
            }),
          );
        }

        if (cancelled) return;

        if (result.status === 'mfa_required') {
          rememberAuthReturnTo(postAuthRedirect);
          navigate(withAuthReturnTo('/auth/mfa', postAuthRedirect), { replace: true });
          return;
        }

        setStep(2);
        clearStoredAuthReturnTo();
        navigate(postAuthRedirect, { replace: true });
      } catch (err) {
        if (!cancelled) {
          setError(err instanceof Error ? err.message : t('auth.callback.failed'));
        }
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [location.hash, location.pathname, location.search, postAuthRedirect, navigate, t]);

  const steps = [
    t('auth.callback.stepResponse'),
    t('auth.callback.stepSession'),
    t('auth.callback.stepRedirect'),
  ];

  return (
    <div className="of-panel" style={{ width: '100%', maxWidth: 420, padding: 28, textAlign: 'center' }}>
      <p className="of-eyebrow">{t('auth.callback.badge')}</p>
      <h1 className="of-heading-lg" style={{ marginTop: 6 }}>
        {t('auth.callback.heading')}
      </h1>
      {error ? (
        <>
          <p
            className="of-status-danger"
            style={{ marginTop: 16, padding: '10px 14px', borderRadius: 'var(--radius-md)', fontSize: 13 }}
            role="alert"
          >
            {error}
          </p>
          <Link
            to={withAuthReturnTo('/auth/login', intendedReturnTo)}
            className="of-link"
            style={{ display: 'inline-block', marginTop: 24, fontSize: 13 }}
          >
            {t('auth.callback.back')}
          </Link>
        </>
      ) : (
        <>
          <p className="of-text-muted" style={{ marginTop: 16, fontSize: 13 }}>
            {t('auth.callback.subtitle')}
          </p>
          <div role="status" aria-live="polite" style={{ display: 'grid', gap: 8, marginTop: 18 }}>
            {steps.map((label, index) => (
              <div
                key={label}
                className={index <= step ? 'of-status-info' : ''}
                style={{
                  display: 'grid',
                  gridTemplateColumns: '18px 1fr',
                  alignItems: 'center',
                  gap: 8,
                  padding: '8px 10px',
                  border: '1px solid var(--border-subtle)',
                  borderRadius: 'var(--radius-md)',
                  color: index <= step ? 'var(--status-info)' : 'var(--text-muted)',
                  fontSize: 12,
                  textAlign: 'left',
                }}
              >
                <span
                  aria-hidden="true"
                  style={{
                    width: 8,
                    height: 8,
                    borderRadius: 999,
                    background: index <= step ? 'var(--status-info)' : 'var(--border-default)',
                    justifySelf: 'center',
                  }}
                />
                <span>{label}</span>
              </div>
            ))}
          </div>
        </>
      )}
    </div>
  );
}

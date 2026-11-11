import { useEffect, useState } from 'react';
import type { FormEvent } from 'react';
import { Link, useNavigate } from 'react-router';

import {
  AuthLayout,
  CheckboxField,
  FormAlert,
  ShieldIcon,
  SubmitButton,
  TextField,
} from '../components/auth-layout';
import { ApiError, apiRequest, getFormValue } from '../lib/api';
import {
  clearOpaqueSecrets,
  finishOpaqueRegistration,
  generateAccountKeys,
  startOpaqueRegistration,
} from '../lib/crypto';
import { isPasswordBlocked, loadPasswordBlocklist } from '../lib/password-blocklist';

export function meta() {
  return [
    { title: 'Create account' },
    { name: 'description', content: 'Create a Chat App account' },
  ];
}

interface FormErrors {
  username?: string;
  email?: string;
  password?: string;
  confirmPassword?: string;
  terms?: string;
  form?: string;
}

interface RegistrationStartResult {
  registration_id: string;
  opaque_response: string;
}

const EMAIL_PATTERN = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;
const USERNAME_PATTERN = /^(?=.{4,64}$)(?!_)(?!.*_$)(?!.*__)[a-z0-9_]+$/;

export default function Home() {
  const navigate = useNavigate();
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [errors, setErrors] = useState<FormErrors>({});

  // Warm the cache so submitting does not wait on the blocklist download.
  useEffect(() => {
    void loadPasswordBlocklist().catch(() => undefined);
  }, []);

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();

    const formData = new FormData(event.currentTarget);
    const username = getFormValue(formData, 'username').trim();
    const email = getFormValue(formData, 'email').trim();
    const password = getFormValue(formData, 'password');
    const confirmPassword = getFormValue(formData, 'confirmPassword');

    const acceptedTerms = formData.get('terms') !== null;

    const validationErrors = validate({
      username,
      email,
      password,
      confirmPassword,
      acceptedTerms,
    });
    setErrors(validationErrors);
    if (Object.keys(validationErrors).length > 0) {
      return;
    }

    setIsSubmitting(true);
    try {
      let blocked = false;
      try {
        blocked = await isPasswordBlocked(password);
      } catch (blocklistError) {
        // The blocklist is a client-side safety net, not a security boundary:
        // a failed download must not prevent the user from registering.
        console.warn('Could not check the password against the blocklist', blocklistError);
      }
      if (blocked) {
        setErrors({ password: 'This password is commonly used. Choose a different password.' });
        return;
      }

      const opaque = await startOpaqueRegistration(password);
      const start = await apiRequest<RegistrationStartResult>('/api/auth/register/start', {
        body: { username, email, opaque_request: opaque.request },
      });
      if (typeof start.registration_id !== 'string' || typeof start.opaque_response !== 'string') {
        throw new Error('The server returned an invalid registration response.');
      }

      const wrappedKeys = await generateAccountKeys(password);
      const opaqueResult = await finishOpaqueRegistration(opaque.session, start.opaque_response);

      try {
        await apiRequest('/api/auth/register/finish', {
          body: {
            registration_id: start.registration_id,
            opaque_record: opaqueResult.record,
            public_key: wrappedKeys.publicX25519Key,
            signing_public_key: wrappedKeys.publicEd25519Key,
            wrapped_x25519_private_key: wrappedKeys.wrappedX25519PrivateKey,
            wrapped_ed25519_private_key: wrappedKeys.wrappedEd25519PrivateKey,
            key_wrap_nonce: wrappedKeys.keyWrapNonce,
            key_wrap_salt: wrappedKeys.keyWrapSalt,
            key_wrap_params: wrappedKeys.keyWrapParams,
          },
        });
      } finally {
        clearOpaqueSecrets(opaqueResult);
      }

      void navigate('/login', { state: { registeredUsername: username } });
    } catch (error) {
      setErrors(toFormErrors(error));
    } finally {
      setIsSubmitting(false);
    }
  }

  return (
    <AuthLayout
      icon={<ShieldIcon className="size-5" />}
      title="Create your account"
      subtitle="Your private space starts here."
      footerNote="No ads. No tracking. Just your conversations."
      dense
    >
      <form
        className="space-y-4"
        noValidate
        onSubmit={(event) => {
          void handleSubmit(event);
        }}
      >
        <TextField
          id="username"
          label="Username"
          type="text"
          autoComplete="username"
          placeholder="Choose a username"
          hint="How friends will find you. No phone number needed."
          error={errors.username}
        />

        <TextField
          id="email"
          label="Email"
          type="email"
          autoComplete="email"
          placeholder="Enter your email address"
          error={errors.email}
        />

        <TextField
          id="password"
          label="Password"
          type="password"
          autoComplete="new-password"
          placeholder="Create a strong password"
          hint="Use at least 12 characters and avoid commonly used passwords."
          error={errors.password}
          trailingLock
        />

        <TextField
          id="confirmPassword"
          label="Confirm password"
          type="password"
          autoComplete="new-password"
          placeholder="Enter your password again"
          error={errors.confirmPassword}
          trailingLock
        />

        <div className="sm:pt-1">
          <CheckboxField id="terms" defaultChecked error={errors.terms}>
            I agree to the <span className="font-medium text-accent">Terms of Service</span> and{' '}
            <span className="font-medium text-accent">Privacy Policy</span>.
          </CheckboxField>
        </div>

        {errors.form && <FormAlert>{errors.form}</FormAlert>}

        <SubmitButton
          pending={isSubmitting}
          idleLabel="Create account"
          pendingLabel="Creating account..."
        />

        <p className="text-center text-[13px] text-muted">
          Already have an account?{' '}
          <Link className="font-medium text-accent hover:underline" to="/login">
            Sign in
          </Link>
        </p>
      </form>
    </AuthLayout>
  );
}

function validate(values: {
  username: string;
  email: string;
  password: string;
  confirmPassword: string;
  acceptedTerms: boolean;
}): FormErrors {
  const errors: FormErrors = {};
  if (!USERNAME_PATTERN.test(values.username)) {
    errors.username =
      'Username must be 4-64 characters using lowercase letters, digits, and underscores. Underscores cannot lead, trail, or repeat.';
  }
  if (!EMAIL_PATTERN.test(values.email) || values.email.length > 320) {
    errors.email = 'Enter a valid email address.';
  }
  if (values.password.length < 12) {
    errors.password = 'Password must be at least 12 characters.';
  }
  if (values.confirmPassword !== values.password) {
    errors.confirmPassword = 'Passwords do not match.';
  }
  if (!values.acceptedTerms) {
    errors.terms = 'Accept the Terms of Service and Privacy Policy to continue.';
  }
  return errors;
}

/** Maps a failed registration to field-level messages where the server says which field. */
function toFormErrors(error: unknown): FormErrors {
  if (error instanceof ApiError) {
    const message = sentence(error.message);
    if (error.message.startsWith('email')) {
      return { email: error.status === 409 ? 'This email is already registered.' : message };
    }
    if (error.message.startsWith('username')) {
      return { username: error.status === 409 ? 'This username is already taken.' : message };
    }
    return { form: message };
  }
  return {
    form: error instanceof Error ? error.message : 'Registration failed. Please try again.',
  };
}

function sentence(value: string): string {
  const text = value.charAt(0).toUpperCase() + value.slice(1);
  return text.endsWith('.') ? text : `${text}.`;
}

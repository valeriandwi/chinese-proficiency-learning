import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { z } from 'zod'
import { Link, useNavigate } from 'react-router-dom'
import { useState } from 'react'
import { ApiError } from '../../api/client'
import { useLogin, useRegister } from '../../api/hooks'
import { Button, Card, ErrorNote } from '../../components/ui'

// Client-side validation is for the person typing, not for the server: every rule
// here is re-checked server-side, and a server rejection is rendered against the
// field it names.

const loginSchema = z.object({
  email: z.string().email('Enter a valid email address'),
  password: z.string().min(1, 'Enter your password'),
})

const registerSchema = z.object({
  display_name: z.string().min(1, 'Tell us what to call you').max(60, 'Too long'),
  email: z.string().email('Enter a valid email address'),
  password: z
    .string()
    .min(8, 'At least 8 characters')
    .refine((v) => !/password|12345678|qwerty/i.test(v), 'Avoid common words and sequences'),
  timezone: z.string().min(1),
  daily_goal_minutes: z.coerce.number().int().min(5).max(600),
})

type LoginValues = z.infer<typeof loginSchema>
type RegisterValues = z.infer<typeof registerSchema>

function browserTimezone(): string {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone || 'Asia/Jakarta'
  } catch {
    return 'Asia/Jakarta'
  }
}

export function LoginPage() {
  const navigate = useNavigate()
  const login = useLogin()
  const [formError, setFormError] = useState<string | null>(null)
  const form = useForm<LoginValues>({
    resolver: zodResolver(loginSchema),
    defaultValues: { email: '', password: '' },
  })

  const onSubmit = form.handleSubmit(async (values) => {
    setFormError(null)
    try {
      await login.mutateAsync(values)
      navigate('/', { replace: true })
    } catch (error) {
      setFormError(error instanceof ApiError ? error.message : 'Could not sign in')
    }
  })

  return (
    <div className="mx-auto w-full max-w-md">
      <Card>
        <h1 className="hanzi text-4xl font-extrabold text-ink">你好！</h1>
        <p className="mt-1 text-sm text-ink-2">Sign in to keep your streak going.</p>
        <form className="mt-5 flex flex-col gap-4" onSubmit={onSubmit}>
          <Field label="Email" error={form.formState.errors.email?.message}>
            <input {...form.register('email')} className="input" autoComplete="email" />
          </Field>
          <Field label="Password" error={form.formState.errors.password?.message}>
            <input {...form.register('password')} type="password" className="input" autoComplete="current-password" />
          </Field>
          {formError ? <ErrorNote>{formError}</ErrorNote> : null}
          <Button type="submit" disabled={login.isPending}>
            {login.isPending ? 'Signing in…' : 'Sign in'}
          </Button>
        </form>
        <p className="mt-4 text-sm text-ink-2">
          New here?{' '}
          <Link to="/register" className="font-semibold text-brand-700">
            Create an account
          </Link>
        </p>
      </Card>
    </div>
  )
}

export function RegisterPage() {
  const navigate = useNavigate()
  const register = useRegister()
  const [formError, setFormError] = useState<string | null>(null)
  const form = useForm<RegisterValues>({
    resolver: zodResolver(registerSchema),
    defaultValues: {
      display_name: '',
      email: '',
      password: '',
      timezone: browserTimezone(),
      daily_goal_minutes: 20,
    },
  })

  const onSubmit = form.handleSubmit(async (values) => {
    setFormError(null)
    try {
      await register.mutateAsync(values)
      navigate('/', { replace: true })
    } catch (error) {
      setFormError(error instanceof ApiError ? error.message : 'Could not create the account')
    }
  })

  return (
    <div className="mx-auto w-full max-w-md">
      <Card>
        <h1 className="text-3xl font-extrabold tracking-tight text-ink">Start learning Mandarin</h1>
        <p className="mt-1 text-sm text-ink-2">
          Three fields and you are in. Placement and preferences come next, not first.
        </p>
        <form className="mt-5 flex flex-col gap-4" onSubmit={onSubmit}>
          <Field label="What should we call you?" error={form.formState.errors.display_name?.message}>
            <input {...form.register('display_name')} className="input" />
          </Field>
          <Field label="Email" error={form.formState.errors.email?.message}>
            <input {...form.register('email')} className="input" autoComplete="email" />
          </Field>
          <Field label="Password" error={form.formState.errors.password?.message}>
            <input {...form.register('password')} type="password" className="input" autoComplete="new-password" />
          </Field>
          <div className="grid grid-cols-2 gap-3">
            <Field label="Daily goal (minutes)" error={form.formState.errors.daily_goal_minutes?.message}>
              <input {...form.register('daily_goal_minutes')} type="number" className="input" min={5} max={600} />
            </Field>
            <Field label="Timezone" error={form.formState.errors.timezone?.message}>
              <input {...form.register('timezone')} className="input" />
            </Field>
          </div>
          {formError ? <ErrorNote>{formError}</ErrorNote> : null}
          <Button type="submit" disabled={register.isPending}>
            {register.isPending ? 'Creating…' : 'Create my account'}
          </Button>
        </form>
        <p className="mt-4 text-sm text-ink-2">
          Already learning?{' '}
          <Link to="/login" className="font-semibold text-brand-700">
            Sign in
          </Link>
        </p>
      </Card>
    </div>
  )
}

function Field({ label, error, children }: { label: string; error?: string; children: React.ReactNode }) {
  return (
    <label className="flex flex-col gap-1">
      <span className="text-sm font-bold text-ink-2">{label}</span>
      {children}
      {error ? <span className="text-xs font-semibold text-danger-700">{error}</span> : null}
    </label>
  )
}

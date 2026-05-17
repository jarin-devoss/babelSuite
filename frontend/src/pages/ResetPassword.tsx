import type { ChangeEvent, FormEvent } from 'react'
import { useState } from 'react'
import { FaEye, FaEyeSlash } from 'react-icons/fa6'
import { Link, useSearchParams } from 'react-router-dom'
import AuthField from '../components/AuthField'
import AuthLayout from '../components/AuthLayout'
import { ApiError, resetPassword } from '../lib/api'

export default function ResetPassword() {
  const [searchParams] = useSearchParams()
  const token = searchParams.get('token') ?? ''
  const [password, setPassword] = useState('')
  const [showPassword, setShowPassword] = useState(false)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [done, setDone] = useState(false)

  if (!token) {
    return (
      <AuthLayout
        title='Invalid link'
        subtitle='This password reset link is invalid or has expired.'
        footer={<Link to='/forgot-password'>Request a new link</Link>}
      >
        <div />
      </AuthLayout>
    )
  }

  if (done) {
    return (
      <AuthLayout
        title='Password updated'
        subtitle='Your password has been changed successfully.'
        footer={<Link to='/sign-in'>Sign in with your new password</Link>}
      >
        <div className='auth-message auth-message--info'>
          You can now sign in with your new password.
        </div>
      </AuthLayout>
    )
  }

  const submit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    setError('')
    setLoading(true)
    try {
      await resetPassword(token, password)
      setDone(true)
    } catch (reason) {
      setError(reason instanceof ApiError ? reason.message : 'Something went wrong. Please try again.')
    } finally {
      setLoading(false)
    }
  }

  return (
    <AuthLayout
      title='Choose a new password'
      subtitle='Enter a new password for your account.'
      footer={<>Back to <Link to='/sign-in'>Sign in</Link></>}
    >
      {error && <div className='auth-message auth-message--error'>{error}</div>}
      <form className='auth-form' onSubmit={submit}>
        <AuthField
          label='New Password'
          type={showPassword ? 'text' : 'password'}
          value={password}
          autoComplete='new-password'
          onChange={(event: ChangeEvent<HTMLInputElement>) => setPassword(event.target.value)}
          trailing={
            <button
              type='button'
              className='auth-field__toggle'
              onClick={() => setShowPassword((v) => !v)}
              aria-label={showPassword ? 'Hide password' : 'Show password'}
            >
              {showPassword ? <FaEyeSlash /> : <FaEye />}
            </button>
          }
        />
        <button className='auth-submit' type='submit' disabled={loading}>
          {loading ? 'Resetting...' : 'Reset Password'}
        </button>
      </form>
    </AuthLayout>
  )
}

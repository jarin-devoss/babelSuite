import type { ChangeEvent, FormEvent } from 'react'
import { useState } from 'react'
import { Link } from 'react-router-dom'
import AuthField from '../components/AuthField'
import AuthLayout from '../components/AuthLayout'
import { ApiError, forgotPassword } from '../lib/api'

export default function ForgotPassword() {
  const [email, setEmail] = useState('')
  const [loading, setLoading] = useState(false)
  const [submitted, setSubmitted] = useState(false)
  const [error, setError] = useState('')

  const submit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    setError('')
    setLoading(true)
    try {
      await forgotPassword(email)
      setSubmitted(true)
    } catch (reason) {
      if (reason instanceof ApiError && reason.status === 503) {
        setError(reason.message)
      } else {
        setSubmitted(true)
      }
    } finally {
      setLoading(false)
    }
  }

  return (
    <AuthLayout
      title='Reset your password'
      subtitle='Enter your email and we will send you a link to reset your password.'
      footer={<>Back to <Link to='/sign-in'>Sign in</Link></>}
    >
      {submitted ? (
        <div className='auth-message auth-message--info'>
          If an account exists for that email, you will receive a reset link shortly. Check your inbox.
        </div>
      ) : (
        <>
          {error && <div className='auth-message auth-message--error'>{error}</div>}
          <form className='auth-form' onSubmit={submit}>
            <AuthField
              label='Email Address'
              type='email'
              value={email}
              autoComplete='email'
              onChange={(event: ChangeEvent<HTMLInputElement>) => setEmail(event.target.value)}
            />
            <button className='auth-submit' type='submit' disabled={loading}>
              {loading ? 'Sending...' : 'Send Reset Link'}
            </button>
          </form>
        </>
      )}
    </AuthLayout>
  )
}

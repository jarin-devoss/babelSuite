import { describe, it, expect } from 'vitest'
import { evaluatePasswordStrength } from './password'

describe('evaluatePasswordStrength', () => {
  it('returns score 1 for very short passwords', () => {
    const result = evaluatePasswordStrength('abc')
    expect(result.score).toBe(1)
    expect(result.label).toBe('Needs work')
  })

  it('returns score 1 for empty string', () => {
    const result = evaluatePasswordStrength('')
    expect(result.score).toBe(1)
    expect(result.label).toBe('Needs work')
  })

  it('returns score 2 for a short mixed-case password with a digit', () => {
    // length<8 (+0), length<12 (+0), upper+lower (+1), digit (+1) → internal 2
    const result = evaluatePasswordStrength('Abc1')
    expect(result.score).toBe(2)
    expect(result.label).toBe('Fair')
  })

  it('returns score 3 for a strong 8-char password', () => {
    // length>=8 (+1), upper+lower (+1), digit (+1) → internal 3
    const result = evaluatePasswordStrength('Password1')
    expect(result.score).toBe(3)
    expect(result.label).toBe('Strong')
  })

  it('returns score 3 for a long password without special chars', () => {
    // length>=8 (+1), length>=12 (+1), upper+lower (+1), digit (+1) → internal 4
    const result = evaluatePasswordStrength('MyLongPassword1')
    expect(result.score).toBe(3)
    expect(result.label).toBe('Strong')
  })

  it('returns score 4 for an excellent password with all character classes', () => {
    // length>=8 (+1), length>=12 (+1), upper+lower (+1), digit (+1), special (+1) → internal 5
    const result = evaluatePasswordStrength('MyLongPassword1!')
    expect(result.score).toBe(4)
    expect(result.label).toBe('Excellent')
  })

  it('includes a non-empty hint for every result', () => {
    const inputs = ['a', 'Abc1', 'Password1', 'MyLongPassword1', 'MyLongPassword1!']
    for (const input of inputs) {
      expect(evaluatePasswordStrength(input).hint.length).toBeGreaterThan(0)
    }
  })
})

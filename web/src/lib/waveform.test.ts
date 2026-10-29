import { describe, expect, it } from 'vitest'
import { peaks } from './waveform'

describe('peaks', () => {
  it('takes the loudest sample per slice and scales the loudest bar to 1', () => {
    const samples = new Float32Array([0.1, -0.2, 0.4, -0.8, 0, 0.1, 0.2, -0.4])
    expect(peaks(samples, 4)).toEqual([0.25, 1, 0.125, 0.5])
  })

  it('keeps a silent take flat instead of amplifying the noise floor', () => {
    expect(peaks(new Float32Array(100).fill(0.001), 5)).toEqual([0, 0, 0, 0, 0])
  })

  it('handles fewer samples than bars', () => {
    expect(peaks(new Float32Array([0.5]), 3)).toHaveLength(3)
  })
})

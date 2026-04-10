//! Reed-Solomon codec over GF(2^8) for FX.25 and IL2P.
//!
//! Primitive polynomial: x^8 + x^4 + x^3 + x^2 + 1 (0x11d).
//! Generator roots: α^FCR through α^(FCR+ncheck-1), FCR=1.
//!
//! Based on Phil Karn's libfec decode_rs_char algorithm (used by direwolf).
//! Codeword convention: c[0] is coefficient of x^{n-1} (descending power).

const PRIM_POLY: u32 = 0x11d;
const NN: usize = 255;
const FX25_FCR: usize = 1;

/// GF(2^8) arithmetic tables.
pub struct GfTables {
    pub exp: [u8; 512],
    pub log: [u8; 256],
}

impl GfTables {
    pub fn new() -> Self {
        let mut exp = [0u8; 512];
        let mut log = [0u8; 256];
        let mut x: u32 = 1;
        for i in 0..255 {
            exp[i] = x as u8;
            exp[i + 255] = x as u8;
            log[x as usize] = i as u8;
            x <<= 1;
            if x & 0x100 != 0 { x ^= PRIM_POLY; }
        }
        GfTables { exp, log }
    }

    #[inline]
    pub fn mul(&self, a: u8, b: u8) -> u8 {
        if a == 0 || b == 0 { 0 }
        else { self.exp[self.log[a as usize] as usize + self.log[b as usize] as usize] }
    }

    #[inline]
    pub fn inv(&self, a: u8) -> u8 {
        if a == 0 { 0 } else { self.exp[255 - self.log[a as usize] as usize] }
    }
}

/// Reed-Solomon codec.
pub struct RsCodec {
    pub(crate) ncheck: usize,
    fcr: usize,
    gf: GfTables,
    gen_poly: Vec<u8>,
}

impl RsCodec {
    /// Create RS codec with FCR=1 (FX.25 convention).
    pub fn new(ncheck: usize) -> Self {
        Self::with_fcr(ncheck, FX25_FCR)
    }

    /// Create RS codec with specified first consecutive root.
    pub fn with_fcr(ncheck: usize, fcr: usize) -> Self {
        let gf = GfTables::new();
        let mut gen = vec![0u8; ncheck + 1];
        gen[0] = 1;
        for i in 0..ncheck {
            let root = gf.exp[fcr + i];
            for j in (1..=i + 1).rev() {
                gen[j] = gen[j - 1] ^ gf.mul(gen[j], root);
            }
            gen[0] = gf.mul(gen[0], root);
        }
        RsCodec { ncheck, fcr, gf, gen_poly: gen }
    }

    pub fn encode(&self, data: &[u8]) -> Vec<u8> {
        let full_data_len = NN - self.ncheck;
        let working = if data.len() < full_data_len {
            let mut v = vec![0u8; full_data_len - data.len()];
            v.extend_from_slice(data);
            v
        } else {
            data.to_vec()
        };

        let mut rem = vec![0u8; self.ncheck];
        for &byte in &working {
            let fb = byte ^ rem[self.ncheck - 1];
            if fb != 0 {
                for j in (1..self.ncheck).rev() {
                    rem[j] = rem[j - 1] ^ self.gf.mul(fb, self.gen_poly[j]);
                }
                rem[0] = self.gf.mul(fb, self.gen_poly[0]);
            } else {
                for j in (1..self.ncheck).rev() {
                    rem[j] = rem[j - 1];
                }
                rem[0] = 0;
            }
        }
        rem.reverse();
        rem
    }

    /// Compute syndromes.
    /// Evaluates the codeword polynomial at each generator root α^(FCR+i).
    /// work[] is descending: work[0] = coeff of x^{n-1}.
    fn syndromes(&self, work: &[u8]) -> (Vec<u8>, bool) {
        let mut syn = vec![0u8; self.ncheck];
        let mut has_err = false;
        for i in 0..self.ncheck {
            let alpha = self.gf.exp[self.fcr + i];
            let mut s = 0u8;
            for &c in work {
                s = self.gf.mul(s, alpha) ^ c;
            }
            syn[i] = s;
            if s != 0 { has_err = true; }
        }
        (syn, has_err)
    }

    /// Decode using the Phil Karn / libfec algorithm.
    /// The block is in descending power order (matching our codeword convention).
    pub fn decode(&self, block: &mut Vec<u8>) -> Option<bool> {
        let block_len = block.len();
        let pad = NN - block_len;

        // Build full-length work array (zero-padded at front = high powers)
        let mut data = vec![0u8; pad];
        data.extend_from_slice(block);

        let (syn, has_err) = self.syndromes(&data);
        if !has_err { return Some(true); }

        // From here, work in ascending power order (libfec convention).
        // data_asc[i] = coefficient of x^i = data[NN-1-i]
        // We don't actually need to reverse the full array — we just need
        // to track positions in ascending convention.
        //
        // In ascending convention:
        //   Syndrome S_i = Σ_j data_asc[j] · α^{(FCR+i)·j}
        //   Error at ascending position j means data_asc[j] is wrong.
        //   Ascending position j = NN-1-descending_position.

        // Berlekamp-Massey to find error locator Λ(x)
        let lambda = self.berlekamp_massey(&syn)?;
        let num_errors = lambda.len() - 1;

        // Chien search: find roots of Λ(x).
        // Λ(α^{-j}) = 0 means error at ascending position j.
        // Equivalently, Λ(α^{255-j}) = 0 means error at ascending position j.
        // We search i = 1..=NN and check Λ(α^i); if zero, error at
        // ascending pos = NN - i (= 255 - i).
        let mut err_pos_asc = Vec::with_capacity(num_errors);
        for i in 1..=NN {
            let mut val = lambda[0];
            for j in 1..lambda.len() {
                val ^= self.gf.mul(lambda[j], self.gf.exp[(i * j) % 255]);
            }
            if val == 0 {
                let asc_pos = NN - i;
                err_pos_asc.push(asc_pos);
            }
        }
        if err_pos_asc.len() != num_errors { return None; }

        // Check all error positions are in the actual data region.
        // In ascending order, positions 0..block_len have real data;
        // positions block_len..NN are virtual zero-padding.
        for &p in &err_pos_asc {
            if p >= block_len { return None; }
        }

        // Error evaluator Ω(x) = S(x)·Λ(x) mod x^ncheck
        // where S(x) = S_0 + S_1·x + S_2·x^2 + ...
        let mut omega = vec![0u8; self.ncheck];
        for i in 0..self.ncheck {
            for j in 0..lambda.len() {
                if j <= i {
                    omega[i] ^= self.gf.mul(syn[i - j], lambda[j]);
                }
            }
        }

        // Forney: compute error magnitudes.
        // For each error at ascending position j, with X_j = α^j:
        //   e_j = X_j^{1-FCR} · Ω(X_j^{-1}) / Λ'(X_j^{-1})
        for &asc_pos in &err_pos_asc {
            // X_j = α^{asc_pos}, X_j^{-1} = α^{255-asc_pos}
            let xinv = self.gf.exp[255 - (asc_pos % 255)]; // α^{-j}

            // Ω(X_j^{-1})
            let mut omega_val = omega[0];
            let mut xp = xinv;
            for k in 1..self.ncheck {
                omega_val ^= self.gf.mul(omega[k], xp);
                xp = self.gf.mul(xp, xinv);
            }

            // Λ'(X_j^{-1}): formal derivative, only odd-indexed terms
            let mut lprime = 0u8;
            xp = 1u8;
            for k in (1..lambda.len()).step_by(2) {
                lprime ^= self.gf.mul(lambda[k], xp);
                xp = self.gf.mul(xp, self.gf.mul(xinv, xinv));
            }
            if lprime == 0 { return None; }

            // e = X_j^{1-FCR} · Ω(X_j^{-1}) / Λ'(X_j^{-1})
            let xj_power = if self.fcr == 0 {
                // X_j^1 = α^{asc_pos}
                self.gf.exp[asc_pos % 255]
            } else {
                // General: X_j^{1-FCR} = α^{asc_pos*(1-FCR)}
                // For FCR=1: X_j^0 = 1
                let exp = (asc_pos as isize * (1 - self.fcr as isize)).rem_euclid(255) as usize;
                self.gf.exp[exp]
            };
            let mag = self.gf.mul(xj_power, self.gf.mul(omega_val, self.gf.inv(lprime)));

            // Convert ascending position to descending position in data[]
            let desc_pos = NN - 1 - asc_pos;
            data[desc_pos] ^= mag;
        }

        // Verify correction
        let (_, still_err) = self.syndromes(&data);
        if still_err { return None; }

        block.copy_from_slice(&data[pad..]);
        Some(true)
    }

    fn berlekamp_massey(&self, syn: &[u8]) -> Option<Vec<u8>> {
        let n = self.ncheck;
        let mut lambda = vec![0u8; n + 1];
        lambda[0] = 1;
        let mut b = vec![0u8; n + 1];
        b[0] = 1;
        let mut l = 0usize;
        let mut delta_b = 1u8;
        let mut r = 1i32;

        for k in 0..n {
            let mut delta = syn[k];
            for i in 1..=l {
                delta ^= self.gf.mul(lambda[i], syn[k - i]);
            }

            if delta == 0 {
                r += 1;
            } else if 2 * l <= k {
                let t = lambda.clone();
                let coeff = self.gf.mul(delta, self.gf.inv(delta_b));
                for i in r as usize..=n {
                    lambda[i] ^= self.gf.mul(coeff, b[i - r as usize]);
                }
                b = t;
                l = k + 1 - l;
                delta_b = delta;
                r = 1;
            } else {
                let coeff = self.gf.mul(delta, self.gf.inv(delta_b));
                for i in r as usize..=n {
                    lambda[i] ^= self.gf.mul(coeff, b[i - r as usize]);
                }
                r += 1;
            }
        }

        if l > self.ncheck / 2 { return None; }

        let mut deg = 0;
        for i in (0..=n).rev() {
            if lambda[i] != 0 { deg = i; break; }
        }
        lambda.truncate(deg + 1);
        Some(lambda)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn gf_tables_consistent() {
        let gf = GfTables::new();
        assert_eq!(gf.exp[0], 1);
        assert_eq!(gf.exp[255], 1);
        assert_eq!(gf.log[1], 0);
    }

    #[test]
    fn gf_mul_identity() {
        let gf = GfTables::new();
        for a in 0..=255u8 {
            assert_eq!(gf.mul(a, 1), a);
            assert_eq!(gf.mul(1, a), a);
            assert_eq!(gf.mul(a, 0), 0);
        }
    }

    #[test]
    fn gf_inv_consistent() {
        let gf = GfTables::new();
        for a in 1..=255u8 {
            let inv = gf.inv(a);
            assert_eq!(gf.mul(a, inv), 1, "a={}, inv={}", a, inv);
        }
    }

    #[test]
    fn rs_encode_syndromes_zero() {
        let rs = RsCodec::new(16);
        let data = vec![0x42u8; 239];
        let check = rs.encode(&data);
        let mut block = Vec::new();
        block.extend_from_slice(&data);
        block.extend_from_slice(&check);
        let (syn, has_err) = rs.syndromes(&block);
        assert!(!has_err, "syndromes not zero: {:?}", syn);
    }

    #[test]
    fn rs_shortened_syndromes_zero() {
        let rs = RsCodec::new(6);
        let data = vec![0x55u8; 26];
        let check = rs.encode(&data);
        let mut work = vec![0u8; NN - 32]; // pad
        work.extend_from_slice(&data);
        work.extend_from_slice(&check);
        let (syn, has_err) = rs.syndromes(&work);
        assert!(!has_err, "syndromes not zero: {:?}", syn);
    }

    #[test]
    fn rs_encode_decode_no_errors() {
        let rs = RsCodec::new(16);
        let data = vec![0x42u8; 239];
        let check = rs.encode(&data);
        let mut block = Vec::new();
        block.extend_from_slice(&data);
        block.extend_from_slice(&check);
        assert_eq!(rs.decode(&mut block), Some(true));
    }

    #[test]
    fn rs_encode_decode_single_error() {
        let rs = RsCodec::new(16);
        let data = vec![0xAA; 239];
        let check = rs.encode(&data);
        let mut block = Vec::new();
        block.extend_from_slice(&data);
        block.extend_from_slice(&check);
        block[100] ^= 0x55;
        assert_eq!(rs.decode(&mut block), Some(true));
        assert_eq!(&block[..239], &vec![0xAA; 239][..]);
    }

    #[test]
    fn rs_encode_decode_with_errors() {
        let rs = RsCodec::new(16);
        let data = vec![0xAA; 239];
        let check = rs.encode(&data);
        let mut block = Vec::new();
        block.extend_from_slice(&data);
        block.extend_from_slice(&check);
        block[0] ^= 0xFF;
        block[10] ^= 0x55;
        block[100] ^= 0x01;
        assert_eq!(rs.decode(&mut block), Some(true));
        assert_eq!(&block[..239], &vec![0xAA; 239][..]);
    }

    #[test]
    fn rs_shortened_code() {
        let rs = RsCodec::new(6);
        let data = vec![0x55u8; 26];
        let check = rs.encode(&data);
        let mut block = Vec::new();
        block.extend_from_slice(&data);
        block.extend_from_slice(&check);
        assert_eq!(rs.decode(&mut block), Some(true));
    }

    #[test]
    fn rs_shortened_with_error() {
        let rs = RsCodec::new(6);
        let data = vec![0x55u8; 26];
        let check = rs.encode(&data);
        let mut block = Vec::new();
        block.extend_from_slice(&data);
        block.extend_from_slice(&check);
        block[10] ^= 0xFF;
        assert_eq!(rs.decode(&mut block), Some(true));
        assert_eq!(&block[..26], &vec![0x55u8; 26][..]);
    }

    #[test]
    fn rs_2_parity_single_error() {
        let rs = RsCodec::new(2);
        let data = vec![0x41u8; 13];
        let check = rs.encode(&data);
        let mut block = Vec::new();
        block.extend_from_slice(&data);
        block.extend_from_slice(&check);
        block[5] ^= 0xFF;
        assert_eq!(rs.decode(&mut block), Some(true));
        assert_eq!(&block[..13], &vec![0x41u8; 13][..]);
    }
}

interface TotpEnrolmentProps {
  secret: string
  qrCodePng: string
}

/**
 * Scanning is the primary path; the secret is kept visible underneath for devices that
 * cannot scan, because retyping a 32-character key is where enrolment usually fails.
 */
export function TotpEnrolment({ secret, qrCodePng }: TotpEnrolmentProps) {
  return (
    <div className="flex flex-col gap-3">
      <p style={{ fontSize: 'var(--text-secondary-size)', color: 'var(--text-secondary)' }}>
        Scan this with your authenticator app, then enter the code it shows.
      </p>

      <div className="flex justify-center">
        <img
          src={qrCodePng}
          alt="QR code for two-factor enrolment"
          width={188}
          height={188}
          className="border"
          style={{
            borderColor: 'var(--border-default)',
            borderRadius: 'var(--radius-sharp)',
            // The code is dark-on-transparent, so it needs a light plate to scan.
            backgroundColor: '#ffffff',
            padding: '10px',
          }}
        />
      </div>

      <details>
        <summary
          className="cursor-pointer select-none"
          style={{ fontSize: 'var(--text-micro)', color: 'var(--text-muted)' }}
        >
          Can't scan? Enter the key manually
        </summary>
        <code
          className="mt-2 block break-all border px-2 py-1.5 font-mono"
          style={{
            fontSize: 'var(--text-micro)',
            borderColor: 'var(--border-default)',
            backgroundColor: 'var(--bg-base)',
            color: 'var(--accent)',
            letterSpacing: '0.06em',
          }}
        >
          {secret}
        </code>
      </details>
    </div>
  )
}

import Grid from '@mui/material/Grid'
import { Card, CardContent, Typography } from '@mui/material'
import type { AnalyticsKpi } from '../../types/analytics'

type Props = {
  items: AnalyticsKpi[]
}

const toneColor: Record<AnalyticsKpi['tone'], string> = {
  ok: 'success.main',
  warn: 'warning.main',
  bad: 'error.main',
  accent: 'primary.main',
  neutral: 'text.primary',
}

export function AnalyticsKpiGrid({ items }: Props) {
  return (
    <Grid container spacing={2}>
      {items.map((item) => (
        <Grid key={item.label} size={{ xs: 12, sm: 6, md: 4 }}>
          <Card variant="outlined" sx={{ height: '100%' }}>
            <CardContent>
              <Typography variant="caption" color="text.secondary" sx={{ fontWeight: 600 }}>
                {item.label}
              </Typography>
              <Typography
                variant="h4"
                sx={{
                  mt: 0.5,
                  fontSize: { xs: '1.5rem', sm: '1.75rem' },
                  fontWeight: 700,
                  color: toneColor[item.tone],
                }}
              >
                {item.value}
              </Typography>
              <Typography variant="body2" color="text.secondary" sx={{ mt: 0.5 }}>
                {item.sub}
              </Typography>
            </CardContent>
          </Card>
        </Grid>
      ))}
    </Grid>
  )
}

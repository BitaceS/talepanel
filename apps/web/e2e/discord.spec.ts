import { generateKeyPairSync, sign } from 'node:crypto'
import { expect, test, type APIRequestContext } from '@playwright/test'
import { accessToken, login } from './helpers'

// The Discord bot without Discord: the settings card, and the interactions
// endpoint's signature check with a key pair made here.

const ENDPOINT = '/api/v1/discord/interactions'

function keyPair() {
  const { publicKey, privateKey } = generateKeyPairSync('ed25519')
  // Raw 32-byte key: the end of the SPKI encoding.
  const raw = publicKey.export({ format: 'der', type: 'spki' }).subarray(-32)
  return { publicHex: raw.toString('hex'), privateKey }
}

async function putSettings(request: APIRequestContext, data: Record<string, unknown>) {
  const token = await accessToken(request)
  return request.put('/api/v1/admin/discord', {
    headers: { Authorization: `Bearer ${token}` },
    data: { enabled: false, application_id: '', public_key: '', guild_id: '', status_channel_id: '',
      status_server_ids: [], show_player_names: false, language: 'en', ...data },
  })
}

test.afterAll(async ({ request }) => {
  await putSettings(request, {})
})

test('the Discord card is on the integrations tab', async ({ page }) => {
  await login(page)
  await page.goto('/admin')
  await page.getByRole('button', { name: 'Integrations' }).click()
  await expect(page.getByRole('heading', { name: 'Discord bot' })).toBeVisible()
  await expect(page.getByText('/api/v1/discord/interactions')).toBeVisible()
})

test('the interactions endpoint checks the signature', async ({ request }) => {
  const { publicHex, privateKey } = keyPair()
  const saved = await putSettings(request, { application_id: '123456789012345678', public_key: publicHex })
  expect(saved.ok(), await saved.text()).toBeTruthy()

  const post = (body: string, signed: boolean) => {
    const ts = Math.floor(Date.now() / 1000).toString()
    const sig = signed ? sign(null, Buffer.from(ts + body), privateKey).toString('hex') : '00'.repeat(64)
    return request.post(ENDPOINT, {
      headers: { 'Content-Type': 'application/json', 'X-Signature-Ed25519': sig, 'X-Signature-Timestamp': ts },
      data: body,
    })
  }

  const unsigned = await post('{"type":1}', false)
  expect(unsigned.status()).toBe(401)

  const ping = await post('{"type":1}', true)
  expect(ping.status()).toBe(200)
  expect(await ping.json()).toEqual({ type: 1 })

  // A command while the bot is off is answered, privately.
  const cmd = await post(JSON.stringify({
    type: 2, application_id: '123456789012345678', guild_id: '223456789012345678',
    member: { user: { id: '111111111111111111', username: 'e2e' } }, data: { name: 'status' }, locale: 'de',
  }), true)
  expect(cmd.status()).toBe(200)
  const res = await cmd.json()
  expect(res.type).toBe(4)
  expect(res.data.flags).toBe(64)
  expect(res.data.content).toContain('ausgeschaltet')
})

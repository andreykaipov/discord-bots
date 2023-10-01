import { verifyKey } from 'discord-interactions'
import { withParams } from 'itty-router'
import { discord } from './helpers.js'

const withDiscordHelper = (request, env) => {
    request.discord = discord(env)
}

const withContent = async (request) => {
    const parseBody = async () => {
        const contentType = request.headers.get('content-type')
        if (contentType?.includes('application/json')) {
            return JSON.stringify(await request.json())
        } else if (contentType?.includes('application/text')) {
            return request.text()
        } else if (contentType?.includes('text/html')) {
            return request.text()
        } else if (contentType?.includes('form')) {
            const formData = await request.formData()
            const body = {}
            for (const entry of formData.entries()) {
                body[entry[0]] = entry[1]
            }
            return JSON.stringify(body)
        } else {
            // Perhaps some other type of data was submitted in the form
            // like an image, or some other binary data.
            return 'binary data'
        }
    }
    request.content = await parseBody()
}

const withVerifyDiscordRequest = (request, env) => {
    request.verifyDiscordRequest = async () => {
        const signature = request.headers.get('x-signature-ed25519')
        const timestamp = request.headers.get('x-signature-timestamp')
        const body = request.content
        const isValidRequest =
            signature &&
            timestamp &&
            verifyKey(body, signature, timestamp, env.DISCORD_PUBLIC_KEY)
        if (!isValidRequest) {
            return { isValid: false }
        }

        return { interaction: JSON.parse(body), isValid: true }
    }
}

export const middleware = [
    withParams,
    withContent,
    withDiscordHelper,
    withVerifyDiscordRequest,
]

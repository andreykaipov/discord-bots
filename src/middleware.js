import { verifyKey } from 'discord-interactions'

export { withParams } from 'itty-router'

export const withContent = async (request) => {
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

export const withDiscordHelper = (request, env) => {
    request.discord = async (method, path, body = {}) => {
        const url = `https://discord.com/api/${path}`
        const response = await fetch(url, {
            method: method,
            body: body,
            headers: {
                Authorization: `Bot ${env.DISCORD_TOKEN}`,
                'Content-Type': 'application/json',
                Accept: 'application/json',
            },
        })
        return response
    }
}

export const withVerifyDiscordRequest = (request, env) => {
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

import { discord } from '.'
import { withParams } from 'itty-router'
import {
    verifyKey,
    InteractionType,
    InteractionResponseType,
} from 'discord-interactions'

export const withDiscordHelper = (request, env) => {
    request.discord = discord(env)
}

export const withContent = async (request) => {
    const contentType = request.headers.get('content-type')

    if (contentType?.includes('application/json')) {
        request.content = await request.json()
        request.contentAsString = JSON.stringify(request.content)
    } else if (contentType?.includes('application/text')) {
        request.content = await request.text()
        request.contentAsString = request.content
    } else if (contentType?.includes('text/html')) {
        request.content = await request.text()
        request.contentAsString = request.body
    } else if (contentType?.includes('application/x-www-form-urlencoded')) {
        request.content = await request.text()
        request.contentAsString = request.body
        console.log(request.content)
    } else if (contentType?.includes('form')) {
        request.content = await request.formData()
        const form = {}
        for (const entry of request.content.entries()) {
            form[entry[0]] = entry[1]
        }
        request.contentAsString = JSON.stringify(form)
    } else {
        // Perhaps some other type of data was submitted in the form
        // like an image, or some other binary data.
        request.content = await request.blob()
        request.contentAsString = 'binary data'
    }
}

export const verifyDiscordKey = async (request, env) => {
    const key = env.DISCORD_PUBLIC_KEY
    const signature = request.headers.get('x-signature-ed25519') || ''
    const timestamp = request.headers.get('x-signature-timestamp') || ''
    const body = request.contentAsString || ''
    const isValidRequest = await verifyKey(body, signature, timestamp, key)

    if (!isValidRequest) {
        return new Response('Bad request signature.', { status: 401 })
    }

    const interaction = JSON.parse(body) || {}
    if (interaction.type === InteractionType.PING) {
        return { type: InteractionResponseType.PONG }
    }
}

export const withDetail = (request) => {
    console.log(request.method, request.url)
    console.log(request.params)
    console.log(request.headers)
    console.log(request.content)
}

export const common = [withParams, withContent, withDiscordHelper]

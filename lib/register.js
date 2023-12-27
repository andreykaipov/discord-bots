import dotenv from 'dotenv'
import process from 'node:process'

/**
 * This file abstracts out the common logic for command registration into one function.
 * When other bots import and use the `register` function, it's only meant to be run once from the
 * command line, and not used in the CF worker. Because of this, it's allowed to use node.js
 * primitives.
 *
 * Note that command registration may take a few minutes to propagate.
 *
 * It expects a list of application commands in the following format:
 *
 * [
 *   {
 *     "name": "ping",
 *     "description": "Replies with pong",
 *     "options": []
 *   },
 *   {
 *     "name": "echo",
 *     "description": "Replies with your input",
 *     "options": [ { ... } ]
 *   }
 * ]
 *
 * See the following docs for more info:
 * https://discord.com/developers/docs/interactions/application-commands#bulk-overwrite-global-application-commands
 * https://discord.com/developers/docs/interactions/application-commands#application-command-object
 */

export const commands = async (commands) => {
    dotenv.config({ path: '.dev.vars' })

    const token = process.env.DISCORD_TOKEN
    const applicationId = process.env.DISCORD_APPLICATION_ID

    if (!token) {
        throw new Error('The DISCORD_TOKEN environment variable is required.')
    }
    if (!applicationId) {
        throw new Error(
            'The DISCORD_APPLICATION_ID environment variable is required.',
        )
    }

    console.log('Registering commands...')

    const url = `https://discord.com/api/v10/applications/${applicationId}/commands`
    const response = await fetch(url, {
        headers: {
            'Content-Type': 'application/json',
            Authorization: `Bot ${token}`,
        },
        method: 'PUT',
        body: JSON.stringify(commands),
    })

    if (response.ok) {
        console.log('Registered all commands')
        const data = await response.json()
        console.log(JSON.stringify(data, null, 2))
        process.exit(0)
    }

    let errorText = `Error registering commands \n ${response.url}: ${response.status} ${response.statusText}`
    try {
        const error = await response.text()
        if (error) {
            errorText = `${errorText} \n\n ${error}`
        }
    } catch (err) {
        console.error('Error reading body from request:', err)
    }
    console.error(errorText)
}

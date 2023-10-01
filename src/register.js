import * as commands from './commands.js'
import dotenv from 'dotenv'
import process from 'node:process'

/**
 * This file is meant to be run from the command line, and is not used by the
 * application server.  It's allowed to use node.js primitives, and only needs
 * to be run once.
 */

dotenv.config({ path: '.dev.vars' })

const token = process.env.DISCORD_TOKEN
const applicationId = process.env.DISCORD_APPLICATION_ID

if (!token) {
    throw new Error('The DISCORD_TOKEN environment variable is required.')
}
if (!applicationId) {
    throw new Error(
        'The DISCORD_APPLICATION_ID environment variable is required.'
    )
}

const id = '1136705165555159060'
const url2 = `https://discord.com/api/channels/${id}/messages`
const response2 = await fetch(url2, {
    method: 'POST',
    headers: {
        'Content-Type': 'application/json',
        Authorization: `Bot ${token}`,
        Accept: 'application/json',
    },
    body: JSON.stringify(
        {
            content: 'Hello, World!',
            tts: false,
            embeds: [
                {
                    title: 'Hello, Embed!',
                    description: 'This is an embedded message.',
                },
            ],
        }
        // {
        //   content: 'hi',
        // }
    ),
})
console.log('response2', response2)

/**
 * Register all commands globally.  This can take o(minutes), so wait until
 * you're sure these are the commands you want.
 */
const url = `https://discord.com/api/v10/applications/${applicationId}/commands`

console.log('hi', commands)

const response = await fetch(url, {
    headers: {
        'Content-Type': 'application/json',
        Authorization: `Bot ${token}`,
    },
    method: 'PUT',
    body: JSON.stringify(Object.values(commands)),
})

if (response.ok) {
    console.log('Registered all commands')
    const data = await response.json()
    console.log(JSON.stringify(data, null, 2))
} else {
    console.error('Error registering commands')
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

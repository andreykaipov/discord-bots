export const discord =
    (env) =>
    async (method, path, body = null) => {
        if (body && typeof body !== 'string') {
            body = JSON.stringify(body)
        }
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

export const chunk = (arr, size = arr.length) => {
    if (arr.length <= size) {
        return [arr]
    }
    return [arr.slice(0, size), ...chunk(arr.slice(size), size)]
}

export const sleep = (ms) => new Promise((r) => setTimeout(r, ms))

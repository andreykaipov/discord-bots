import { sleep, discord } from './helpers.js'
import { table, getBorderCharacters } from 'table'
import * as timeago from 'timeago.js'
import { en_short as locale_en_short } from 'timeago.js/lib/lang/index.js'

timeago.register('en_short', locale_en_short)

const api = 'https://map.starminers.site/data'

// these are are from StarMiners, but they're different times than the ones on the wiki:
// https://oldschool.runescape.wiki/w/Shooting_Stars#Star_sizes
// consider instead 18+18.42+13.16+8.52+8.75+6+4.62+6+3.21?
const minutesUntilDead = {
    0: 0,
    1: 24,
    2: 43,
    3: 57,
    4: 67,
    5: 76,
    6: 83,
    7: 89,
    8: 94,
    9: 99,
}

// yoinked from StarMiners, just cleaned up a bit
// not entirely sure how this works:
// - how does summing up diffs tells us anything about the next wave?
// - magic numbers 90 and 120?
const calculateNextWave = (stars) => {
    if (stars.length === 0) return 'N/A'
    const min = stars.reduce((a, b) => (a.min < b.min ? a : b)).min
    const max = stars.reduce((a, b) => (a.max > b.max ? a : b)).max
    const diff = (max - min) / 60
    if (diff > 90) return 'NEW ROCKS NOW'

    const now = Date.now() / 1000 // in seconds
    const diffSum = stars.reduce((acc, a) => acc + (now - a.max), 0) / 60
    const diffAvg = diffSum / stars.length
    const estimated = Math.round(120 - diffAvg)
    return `Next wave in approximately ${estimated} minutes`
}

export const getStars = async () => {
    const now = Date.now() / 1000 // in seconds
    const response = await fetch(api)
    const stars = (await response.json())
        .filter((o) => o.minTime !== null && o.maxTime !== null)
        .map((star) => ({
            deadtime: star.calledAt + minutesUntilDead[star.tier] * 60,
            tier: star.tier,
            world: star.world,
            location: star.calledLocation.slice(0, 40),
            called: star.calledAt,
            calledBy: star.calledBy,
            min: star.minTime,
            max: star.maxTime,
        }))
        .filter((star) => star.deadtime > now)
        .sort((a, b) => b.deadtime - a.deadtime || b.tier - a.tier)
        .map((star) => {
            star.called = timeago.format(star.called * 1000, 'en_short')
            star.deadtime = timeago.format(star.deadtime * 1000, 'en_short')
            return star
        })
    const nextWave = calculateNextWave(stars)
    const headers = [
        [nextWave, '', '', '', '', ''],
        ['Est. Dead', 'Tier', 'World', 'Location', 'Called', 'Called By'],
    ]
    const data = stars.map((star) => {
        delete star.min
        delete star.max
        return Object.values(star)
    })
    return table([...headers, ...data], {
        border: getBorderCharacters('norc'),
        columns: [
            { alignment: 'right' },
            { alignment: 'right' },
            { alignment: 'right' },
            { alignment: 'left' },
            { alignment: 'right' },
            { alignment: 'left' },
        ],
        spanningCells: [{ col: 0, row: 0, colSpan: 6, alignment: 'center' }],
        columnDefault: {
            paddingLeft: 0,
            paddingRight: 2,
        },
        drawHorizontalLine: (lineIndex, rowCount) => {
            return lineIndex === 1 // lineIndex === 0 || lineIndex === rowCount
        },
        drawVerticalLine: (lineIndex, columnCount) => {
            return false // lineIndex === 0 || lineIndex === columnCount
        },
    })
}

// 1. deletes all messages in a channel
// 2. prints stars in chunks of 20 lines
export const updateChannels = async (env, channels) => {
    const stars = await getStars()
    const starlines = stars.match(/(?=[\s\S])(?:.*\n?){1,20}/g) // split every 20th new line
    console.log(starlines)
    for await (const channel of channels) {
        const response = await discord(env)(
            'GET',
            `channels/${channel}/messages?limit=100`,
        )
        const messages = await response.json()
        for await (const message of messages) {
            const response = await discord(env)(
                'DELETE',
                `channels/${channel}/messages/${message.id}`,
            )
            console.log(response.status)
            await sleep(500)
        }

        for (const chunk of starlines) {
            const body = { content: `\`\`\`${chunk}\`\`\`` }
            await discord(env)('POST', `channels/${channel}/messages`, body)
        }
    }
    console.log('end of updateChannels')
}

import * as timeago from 'timeago.js'
import { en_short as locale_en_short } from 'timeago.js/lib/lang/index.js'
import { table, getBorderCharacters } from 'table'
timeago.register('en_short', locale_en_short)

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

export const getStars = async () => {
    const response = await fetch('https://map.starminers.site/data')
    const stars = (await response.json())
        .map((star) => {
            return {
                called: star.calledAt * 1000,
                tier: star.tier,
                world: star.world,
                location: star.calledLocation,
                'called by': star.calledBy,
                'est. dead':
                    star.calledAt * 1000 + minutesUntilDead[star.tier] * 60000,
            }
        })
        .filter((star) => star.tier > 3)
    stars.sort((a, b) => b.tier - a.tier || b.called - a.called)
    const formatted = stars.map((star) => {
        star.called = timeago.format(star.called, 'en_short')
        star['est. dead'] = timeago.format(star['est. dead'], 'en_short')
        return star
    })
    return table([Object.keys(stars[0]), ...formatted.map(Object.values)], {
        border: getBorderCharacters('norc'),
        columnDefault: {
            paddingLeft: 0,
            paddingRight: 2,
        },
        drawHorizontalLine: (lineIndex, rowCount) => {
            return lineIndex === 1
            return lineIndex === 0 || lineIndex === rowCount
        },
        drawVerticalLine: (lineIndex, columnCount) => {
            return false
            return lineIndex === 0 || lineIndex === columnCount
        },
    })
}

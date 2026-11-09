import pxtorem from "postcss-pxtorem";

/**
 * Styles are written in the exact pixel values from Figma and converted to rem
 * at build time, so the whole dashboard scales with the root font size.
 * 1px borders stay crisp because of minPixelValue.
 */
export default {
  plugins: [
    pxtorem({
      rootValue: 16,
      propList: ["*"],
      minPixelValue: 2,
      selectorBlackList: ["html"],
      exclude: /node_modules/i,
    }),
  ],
};
